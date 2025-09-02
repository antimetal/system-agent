// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config

import "google.golang.org/protobuf/proto"

// Status represents the status of a configuration operation.
type Status uint8

const (
	// StatusOK indicates the configuration was acknowledged/accepted.
	StatusOK Status = 1 << iota
	// StatusInvalid indicates the configuration was not acknowledged/rejected.
	StatusInvalid
)

// Instance of a config object that includes its status.
type Instance struct {
	TypeUrl string
	Name    string
	Version string
	Object  proto.Message
	Status  Status
	Expired bool
}

// Copy returns a deep copy of the Instance.
func (i *Instance) Copy() Instance {
	return Instance{
		TypeUrl: i.TypeUrl,
		Name:    i.Name,
		Version: i.Version,
		Object:  proto.Clone(i.Object),
		Status:  i.Status,
		Expired: i.Expired,
	}
}

// Filters includes optional parameters to filter configs.
type Filters struct {
	// Types filters for config types to watch for.
	// If empty, then defaults for all types.
	Types []string
	// Bitmask of config statuses to watch for e.g. StatusOK | StatusInvalid
	// If unset, defaults to StatusOK.
	Status Status
}

// Options when retrieving configs.
type Options struct {
	Filters Filters
}

// Loader retrieves configs.
type Loader interface {
	// ListConfigs retrieves available configs with optional filters.
	//
	// If Loader receives a new version of a config that is invalid, ListConfigs
	// SHOULD return the most recent valid config, otherwise it will return the
	// current config with StatusInvalid.
	ListConfigs(opts Options) (map[string][]Instance, error)
	// GetConfig gets a config object identified as name of type configType.
	// It returns an error if no config is found.
	//
	// If Loader receives a new version of a config that is invalid, GetConfig
	// SHOULD return the most recent valid config, otherwise it will return the
	// current config with StatusInvalid.
	GetConfig(configType, name string) (Instance, error)
	// Watch returns a channel that receives configuration objects as they change.
	// Each invocation of Watch returns a separate channel instance.
	//
	// Instances received on the channel SHOULD have at minimum AT LEAST ONCE
	// semantics - duplicate Instances may be received on the channel. The client
	// is responsible for handling potentially duplicate Instances.
	//
	// The channel SHOULD NOT send an Instance with a Version lower than a
	// previously received Instance with the same TypeUrl and Name.
	//
	// If the Loader instance is also a LoaderCloser, the channel SHOULD be
	// closed once Close() is called. If the loader is closed, then this
	// SHOULD return a nil channel.
	Watch(opts Options) <-chan Instance
}

// LoaderCloser groups the Loader methods with Close.
type LoaderCloser interface {
	Loader

	// Close stops the loader and cleans up resources.
	// This method SHOULD BE IDEMPOTENT - Calling Close multiple times SHOULD
	// return the error of the first close call.
	Close() error
}
