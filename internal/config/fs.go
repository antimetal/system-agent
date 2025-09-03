// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"

	typesv1 "github.com/antimetal/agent/pkg/api/antimetal/types/v1"
)

type FSLoader struct {
	mu sync.RWMutex

	basePath string
	watcher  *fsnotify.Watcher
	logger   logr.Logger
	done     chan struct{}
	wg       sync.WaitGroup
	subs     subscriptions

	cache map[string]Instance
}

func NewFSLoader(basePath string, logger logr.Logger) (*FSLoader, error) {
	fsLogger := logger.WithName("config.loader.fs")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem watcher: %w", err)
	}
	closeWatcher := func() {
		if err := watcher.Close(); err != nil {
			fsLogger.Error(err, "failed to close fs watcher")
		}
	}

	if err := addWatches(watcher, basePath, fsLogger); err != nil {
		defer closeWatcher()
		return nil, fmt.Errorf("failed to add watches: %w", err)
	}

	fl := &FSLoader{
		basePath: basePath,
		watcher:  watcher,
		logger:   fsLogger,
		done:     make(chan struct{}, 1),
		cache:    make(map[string]Instance),
	}

	// Scan existing files to populate cache.
	if err := fl.initLoadFiles(); err != nil {
		defer closeWatcher()
		return nil, fmt.Errorf("failed to scan existing config files: %w", err)
	}

	fl.wg.Add(1)
	go fl.processEvents()

	return fl, nil
}

func (fl *FSLoader) initLoadFiles() error {
	return filepath.WalkDir(fl.basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fl.logger.V(1).Info("skipping path with error", "path", path, "error", err)
			return nil
		}

		if d.IsDir() || !fl.isConfigFile(path) {
			return nil
		}

		// Load and cache existing config files
		_, err = fl.loadConfigFile(path)
		if err != nil {
			fl.logger.Error(err, "failed to load existing config file at startup", "path", path)
		}

		return nil
	})
}

func (fl *FSLoader) Watch(opts Options) <-chan Instance {
	ch := fl.subs.add(opts.Filters)
	// ch will be nil if closed
	if ch == nil {
		return ch
	}

	fl.wg.Add(1)
	go func() {
		defer fl.wg.Done()

		configs, err := fl.ListConfigs(opts)
		if err != nil {
			fl.logger.Error(err, "failed to get current configs for watch")
			return
		}

		for _, instances := range configs {
			for _, instance := range instances {
				select {
				case ch <- instance:
				case <-fl.done:
					return
				}
			}
		}
	}()

	return ch
}

func (fl *FSLoader) ListConfigs(opts Options) (map[string][]Instance, error) {
	configs := make(map[string][]Instance)

	fl.mu.RLock()
	defer fl.mu.RUnlock()

	for _, instance := range fl.cache {
		if !Matches(instance, opts.Filters) {
			continue
		}

		configType := instance.TypeUrl
		configs[configType] = append(configs[configType], instance)
	}

	return configs, nil
}

func (fl *FSLoader) GetConfig(configType, name string) (Instance, error) {
	cacheKey := fl.getCacheKey(configType, name)

	fl.mu.RLock()
	instance, exists := fl.cache[cacheKey]
	fl.mu.RUnlock()

	if !exists {
		return Instance{}, fmt.Errorf("config %s of type %s not found", name, configType)
	}
	return instance, nil
}

func (fl *FSLoader) Close() error {
	close(fl.done)
	fl.wg.Wait()
	fl.subs.close()
	return fl.watcher.Close()
}

func (fl *FSLoader) processEvents() {
	defer fl.wg.Done()
	for {
		select {
		case <-fl.done:
			return
		case event, ok := <-fl.watcher.Events:
			if !ok {
				return
			}
			fl.handleEvent(event)
		case err, ok := <-fl.watcher.Errors:
			if !ok {
				return
			}
			fl.logger.Error(err, "filesystem watcher error")
		}
	}
}

func (fl *FSLoader) handleEvent(event fsnotify.Event) {
	if !fl.isConfigFile(event.Name) {
		return
	}

	fl.logger.V(1).Info("received file event", "file", event.Name, "op", event.Op)

	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		fl.processConfigFile(event.Name)
	}
}

func (fl *FSLoader) isConfigFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".json" || ext == ".yaml" || ext == ".yml"
}

func (fl *FSLoader) processConfigFile(filename string) {
	instance, err := fl.loadConfigFile(filename)
	if err != nil {
		fl.logger.Error(err, "failed to load config file", "path", filename)
	}
	// Always send instance to subscriptions, even if invalid
	fl.subs.send(instance)
}

func (fl *FSLoader) loadConfigFile(filename string) (Instance, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Instance{Status: StatusInvalid}, fmt.Errorf("failed to read config file: %w", err)
	}

	if len(data) == 0 {
		return Instance{Status: StatusInvalid}, fmt.Errorf("config file is empty")
	}

	obj, err := fl.unmarshalObject(data, filename)
	if err != nil {
		return Instance{Status: StatusInvalid}, fmt.Errorf("failed to unmarshal config file: %w", err)
	}

	instance, parseErr := Parse(obj)

	cacheKey := fl.getCacheKey(instance.TypeUrl, instance.Name)
	fl.mu.Lock()
	defer fl.mu.Unlock()
	prevInstance := fl.cache[cacheKey]

	if parseErr == nil || prevInstance.Object == nil {
		fl.cache[cacheKey] = instance
	}

	return instance, parseErr
}

func (fl *FSLoader) getCacheKey(configType, name string) string {
	return configType + ":" + name
}

func (fl *FSLoader) unmarshalObject(data []byte, filename string) (*typesv1.Object, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	obj := &typesv1.Object{}

	switch ext {
	case ".json":
		if err := protojson.Unmarshal(data, obj); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
		}
	case ".yaml", ".yml":
		var yamlData any
		if err := yaml.Unmarshal(data, &yamlData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
		}

		jsonData, err := json.Marshal(yamlData)
		if err != nil {
			return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}

		if err := protojson.Unmarshal(jsonData, obj); err != nil {
			return nil, fmt.Errorf("failed to unmarshal converted JSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	return obj, nil
}

func addWatches(watcher *fsnotify.Watcher, path string, logger logr.Logger) error {
	return filepath.WalkDir(path, func(walkPath string, d fs.DirEntry, err error) error {
		if err != nil {
			logger.V(1).Info("skipping path with error", "path", walkPath, "error", err)
			return nil
		}

		if d.IsDir() {
			if err := watcher.Add(walkPath); err != nil {
				return err
			}
			logger.V(1).Info("watching directory", "path", walkPath)
		}

		return nil
	})
}
