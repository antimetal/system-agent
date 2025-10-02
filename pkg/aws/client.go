// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package aws

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/go-logr/logr"
)

type Client interface {
	// GetRegion returns the AWS region
	GetRegion(ctx context.Context) (string, error)

	// GetAccountID returns the AWS account ID
	GetAccountID(ctx context.Context) (string, error)

	// GetEKSClusterName returns the name of the EKS cluster
	GetEKSClusterName(ctx context.Context) (string, error)

	// GetEC2InstanceID returns the EC2 instance ID
	GetEC2InstanceID(ctx context.Context) (string, error)
}

var (
	eksClusterTags = []string{
		"aws:eks:cluster-name",
		"eks:cluster-name",
	}

	clusterTagPrefixes = []string{
		"kubernetes.io/cluster/",
		"k8s.io/cluster/",
	}
)

var _ Client = &client{}

type ClientOption func(c *client) error

func WithLogger(logger logr.Logger) ClientOption {
	return func(c *client) error {
		c.logger = logger
		return nil
	}
}

func WithRegion(region string) ClientOption {
	return func(c *client) error {
		c.region = region
		return nil
	}
}

func WithAccountID(accountID string) ClientOption {
	return func(c *client) error {
		c.accountID = accountID
		return nil
	}
}

func WithEKSClusterName(clusterName string) ClientOption {
	return func(c *client) error {
		c.eksClusterName = clusterName
		return nil
	}
}

func WithAutoDiscovery(ctx context.Context) ClientOption {
	return func(c *client) error {
		imdsCfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return fmt.Errorf("error loading default AWS config for IMDS client: %w", err)
		}
		c.imdsClient = imds.NewFromConfig(imdsCfg)

		if c.region == "" {
			resp, err := c.imdsClient.GetRegion(ctx, &imds.GetRegionInput{})
			if err != nil {
				return fmt.Errorf("error auto-discovering region: %w", err)
			}
			c.region = resp.Region
		}

		ec2Cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(c.region))
		if err != nil {
			return fmt.Errorf("error loading default AWS config for EC2 client: %w", err)
		}
		c.ec2Client = ec2.NewFromConfig(ec2Cfg)
		return nil
	}
}

// NewClient returns a new AWS client.
// The returned client is not safe to use in concurrent go routines.
func NewClient(opts ...ClientOption) (Client, error) {
	c := &client{}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

type client struct {
	logger logr.Logger

	ec2Client  *ec2.Client
	imdsClient *imds.Client

	accountID      string
	region         string
	eksClusterName string
	instanceID     string
}

func (c *client) GetRegion(ctx context.Context) (string, error) {
	if c.region != "" {
		return c.region, nil
	}

	if c.imdsClient == nil {
		return "", fmt.Errorf("cannot auto-discover region: " +
			"initialize Client with WithRegion or WithAutoDiscovery")
	}

	resp, err := c.imdsClient.GetRegion(ctx, &imds.GetRegionInput{})
	if err != nil {
		return "", fmt.Errorf("cannot auto-discover region: %w", err)
	}
	c.region = resp.Region

	return c.region, nil
}

func (c *client) GetAccountID(ctx context.Context) (string, error) {
	if c.accountID != "" {
		return c.accountID, nil
	}

	if c.imdsClient == nil {
		return "", fmt.Errorf("cannot auto-discover account ID: " +
			"initialize Client with WithAccountID or WithAutoDiscovery")
	}

	resp, err := c.imdsClient.GetInstanceIdentityDocument(ctx, &imds.GetInstanceIdentityDocumentInput{})
	if err != nil {
		return "", fmt.Errorf("cannot auto-discover account ID: %w", err)
	}

	c.accountID = resp.AccountID

	return c.accountID, nil
}

func (c *client) GetEKSClusterName(ctx context.Context) (string, error) {
	if c.eksClusterName != "" {
		return c.eksClusterName, nil
	}

	if c.imdsClient == nil {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: " +
			"initialize Client with WithEKSClusterName or WithAutoDiscovery")
	}

	if c.ec2Client == nil {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: " +
			"initialize Client with WithEKSClusterName or WithAutoDiscovery")
	}

	instanceID, err := c.getMetadata(ctx, "instance-id")
	if err != nil {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: "+
			"cannot get instanceID from IMDS server: %w", err)
	}

	resp, err := c.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: "+
			"cannot describe instances: %w", err)
	}

	if len(resp.Reservations) != 1 {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: "+
			"expected 1 found EC2 reservation, got %d", len(resp.Reservations))
	}

	if len(resp.Reservations[0].Instances) != 1 {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: "+
			"expected 1 found EC2 instance, got %d", len(resp.Reservations[0].Instances))
	}

	if len(resp.Reservations[0].Instances[0].Tags) == 0 {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: "+
			"no tags found for EC2 instance: %s", instanceID)
	}

	clusterName := c.findClusterNameFromTags(resp.Reservations[0].Instances[0].Tags)
	if clusterName == "" {
		return "", fmt.Errorf("cannot auto-discover EKS cluster name: "+
			"cannot find tag that contains cluster name for instanceID: %s", instanceID)
	}

	c.eksClusterName = clusterName

	return c.eksClusterName, nil
}

func (c *client) getMetadata(ctx context.Context, path string) (string, error) {
	if c.imdsClient == nil {
		return "", fmt.Errorf("initialize Client with WithAutoDiscovery")
	}

	resp, err := c.imdsClient.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: path,
	})
	if err != nil {
		return "", err
	}

	defer func() {
		if err := resp.Content.Close(); err != nil {
			c.logger.Error(err, "cannot close metadata content")
		}
	}()
	bytes, err := io.ReadAll(resp.Content)
	if err != nil {
		return "", fmt.Errorf("cannot read metadata content: %w", err)
	}
	return string(bytes), nil
}

func (c *client) GetEC2InstanceID(ctx context.Context) (string, error) {
	if c.instanceID != "" {
		return c.instanceID, nil
	}

	if c.imdsClient == nil {
		return "", fmt.Errorf("cannot auto-discover EC2 instance ID: " +
			"initialize Client with WithAutoDiscovery")
	}

	instanceID, err := c.getMetadata(ctx, "instance-id")
	if err != nil {
		return "", fmt.Errorf("cannot auto-discover EC2 instance ID: %w", err)
	}

	c.instanceID = instanceID
	return c.instanceID, nil
}

func (c *client) findClusterNameFromTags(tags []ec2Types.Tag) string {
	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}

		for _, eksTag := range eksClusterTags {
			if *tag.Key == eksTag {
				return *tag.Value
			}
		}

		for _, k8sTag := range clusterTagPrefixes {
			if strings.HasPrefix(*tag.Key, k8sTag) && *tag.Value != "owned" {
				return strings.TrimPrefix(*tag.Key, k8sTag)
			}
		}
	}
	return ""
}
