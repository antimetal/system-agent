// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/internal/resource"
	"github.com/antimetal/agent/pkg/errors"
	"github.com/go-logr/logr"
	gogoproto "github.com/gogo/protobuf/proto"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/antimetal/agent/internal/kubernetes/cluster"
)

var _ manager.LeaderElectionRunnable = (*controller)(nil)

// +kubebuilder:rbac:groups=apps,resources=daemonsets;deployments;replicasets;statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets/status;deployments/status;replicasets/status;statefulsets/status,verbs=get
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch,resourceNames=cluster-info
// +kubebuilder:rbac:groups=core,resources=nodes;persistentvolumes;persistentvolumeclaims;pods;services,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes/status;persistentvolumes/status;persistentvolumeclaims/status;replicationcontrollers/status;services/status,verbs=get

const (
	controllerName = "k8s-agent"
	prefixKey      = "kubernetes"

	maxConcurrentIndexers = 1
)

type object interface {
	gogoproto.Message
	gogoproto.Marshaler
	gogoproto.Unmarshaler
	metav1.Object
	runtime.Object
}

var (
	resourcesToWatch = []object{
		&corev1.Node{},
		&corev1.Pod{},
		&corev1.PersistentVolume{},
		&corev1.PersistentVolumeClaim{},
		&corev1.Service{},
		&appsv1.DaemonSet{},
		&appsv1.Deployment{},
		&appsv1.ReplicaSet{},
		&appsv1.StatefulSet{},
		&batchv1.Job{},
	}
)

// Collector builds a snapshot of the state of the cluster
type Controller struct {
	Config    *rest.Config
	K8sClient client.Client
	Provider  cluster.Provider
	Store     resource.Store
}

// SetupWithManger registers the Controller to the provided manager
func (c *Controller) SetupWithManager(mgr manager.Manager) error {
	if mgr == nil {
		return fmt.Errorf("must provide a non-nil Manager")
	}
	if c.Store == nil {
		return fmt.Errorf("Controller must be configured with a non-nil Store")
	}
	if c.K8sClient == nil {
		c.K8sClient = mgr.GetClient()
	}

	if c.Config == nil {
		c.Config = mgr.GetConfig()
	}

	cacheSyncTimeout := mgr.GetControllerOptions().CacheSyncTimeout
	if cacheSyncTimeout == 0 {
		// Use the same default as controller-runtime Controllers
		cacheSyncTimeout = 2 * time.Minute
	}

	ratelimiter := workqueue.DefaultTypedControllerRateLimiter[event]()
	queue := workqueue.NewTypedRateLimitingQueueWithConfig(ratelimiter,
		workqueue.TypedRateLimitingQueueConfig[event]{
			Name: controllerName,
		},
	)

	indexer := &indexer{
		store:    c.Store,
		provider: c.Provider,
	}

	ctrl := &controller{
		cfg:              c.Config,
		scheme:           mgr.GetScheme(),
		provider:         c.Provider,
		logger:           mgr.GetLogger().WithName(controllerName),
		informerFactory:  mgr.GetCache(),
		cacheSyncTimeout: cacheSyncTimeout,
		indexer:          indexer,
		queue:            queue,
	}

	return mgr.Add(ctrl)
}

type controller struct {
	cfg              *rest.Config
	scheme           *runtime.Scheme
	provider         cluster.Provider
	logger           logr.Logger
	informerFactory  cache.Informers
	cacheSyncTimeout time.Duration
	queue            workqueue.TypedRateLimitingInterface[event]
	indexer          *indexer

	// runtime state
	started bool
}

func (c *controller) Start(ctx context.Context) error {
	if c.started {
		return fmt.Errorf("controller was started more than once. This can be caused by being added to a manager multiple times")
	}

	c.logger.V(1).Info("Starting controller")

	major, minor, err := c.getKubeVersion()
	if err != nil {
		return fmt.Errorf("failed to get k8s version: %w", err)
	}
	if err := c.indexer.LoadClusterInfo(ctx, major, minor); err != nil {
		return fmt.Errorf("failed to load cluster info: %w", err)
	}

	if err := c.syncCache(ctx); err != nil {
		return fmt.Errorf("error syncing cache: %w", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < maxConcurrentIndexers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.indexWorker(ctx)
		}()
	}

	c.started = true
	<-ctx.Done()
	c.logger.Info("Shutting down controller")
	c.Shutdown()
	wg.Wait()
	return nil
}

func (c *controller) Shutdown() {
	c.queue.ShutDown()
	c.started = false
}

// Implements sigs.k8s.io/controller-runtime/pkg/manager.LeaderElectionRunnable interface
// Always returns false to disable leader election.
func (c *controller) NeedLeaderElection() bool {
	return false
}

func (c *controller) indexWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			c.indexObjects(ctx)
		}
	}
}

func (c *controller) indexObjects(ctx context.Context) {
	ev, shutdown := c.queue.Get()
	if shutdown {
		return
	}
	defer c.queue.Done(ev)

	var err error
	switch ev.typ {
	case EventAdd:
		c.logger.V(1).Info("adding object to index", "event", eventStr(ev.typ), "object", ev.obj)
		err = c.indexer.Update(ctx, ev.obj)
	case EventUpdate:
		c.logger.V(1).Info("update object in index", "event", eventStr(ev.typ), "object", ev.obj)
		err = c.indexer.Update(ctx, ev.obj)
	case EventDelete:
		c.logger.V(1).Info("deleting object to index", "event", eventStr(ev.typ), "object", ev.obj)
		err = c.indexer.Delete(ctx, ev.obj)
	default:
		err = fmt.Errorf("unknown event type: %d", ev.typ)
	}

	if err != nil {
		errLog := "failed to index object"
		if errors.Retryable(err) {
			// Requeue the event if the returned error is retryable/
			// We'll keep doing this until successful
			errLog += "; will retry"
			c.queue.AddRateLimited(ev)
		}
		c.logger.V(1).Info(errLog, "error", err, "event", eventStr(ev.typ), "object", ev.obj)
		return
	}

	// If we've successfully indexed the object, we can forget it so that it is
	// cleared from requeuing.
	c.queue.Forget(ev)
}

func (c *controller) syncCache(ctx context.Context) error {
	syncCtx, syncCancel := context.WithTimeout(ctx, c.cacheSyncTimeout)
	defer syncCancel()
	g, gCtx := errgroup.WithContext(syncCtx)

	for _, obj := range resourcesToWatch {
		g.Go(func() error {
			var informer cache.Informer
			var err error

			// cache.GetInformer will block until its context is cancelled (e.g timeout) if the cache was
			// already started and it can not sync that informer (most commonly due to RBAC issues).
			if err := wait.PollUntilContextCancel(gCtx, 10*time.Second, true, func(ctx context.Context) (bool, error) {
				informer, err = c.informerFactory.GetInformer(gCtx, obj)
				if err != nil {
					kindMatchErr := &meta.NoKindMatchError{}
					switch {
					case errors.As(err, &kindMatchErr):
						c.logger.Error(err, "kind not found from API server", "kind", kindMatchErr.GroupKind)
					case runtime.IsNotRegisteredError(err):
						c.logger.Error(err, "kind must be registered to the Scheme")
					default:
						c.logger.Error(err, "failed to get informer from cache")
					}
					return false, nil // Retry
				}
				return true, nil
			}); err != nil {
				return err
			}

			gvks, _, err := c.scheme.ObjectKinds(obj)
			if len(gvks) == 0 || err != nil {
				return err
			}

			h := k8sCollectorHandler{
				logger: c.logger,
				scheme: c.scheme,
				queue:  c.queue,
			}
			_, err = informer.AddEventHandler(h)
			if err != nil {
				return fmt.Errorf("failed to add event handler to informer: %w", err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to get informer from cache: %w", err)
	}

	if !c.informerFactory.WaitForCacheSync(syncCtx) {
		return fmt.Errorf("cache did not sync")
	}

	return nil
}

func (c *controller) getKubeVersion() (major, minor string, err error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(c.cfg)
	if err != nil {
		return "", "", fmt.Errorf("failed to create discovery client: %w", err)
	}
	v, err := discoveryClient.ServerVersion()
	if err != nil {
		return "", "", fmt.Errorf("failed to get server version: %w", err)
	}
	return v.Major, v.Minor, nil
}
