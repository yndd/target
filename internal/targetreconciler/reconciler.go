/*
Copyright 2021 NDD.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package targetreconciler

import (
	"context"
	"time"

	gnmictarget "github.com/karimra/gnmic/target"
	"github.com/karimra/gnmic/types"
	"github.com/pkg/errors"
	"github.com/yndd/cache/pkg/cache"
	"github.com/yndd/cache/pkg/origin"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"github.com/yndd/target/pkg/target"
	"google.golang.org/grpc"
)

const (
	// timers
	reconcileTimer = 1 * time.Second

	// errors
	errCreateGnmiClient = "cannot create gnmi client"
)

// Reconciler defines the interfaces for the collector
type Reconciler interface {
	Start() error
	Stop() error
	WithLogger(log logging.Logger)
	WithCache(c cache.Cache)
	WithTarget(t *gnmictarget.Target)
}

// Option can be used to manipulate Options.
type Option func(Reconciler)

// WithLogger specifies how the collector logs messages.
func WithLogger(log logging.Logger) Option {
	return func(r Reconciler) {
		r.WithLogger(log)
	}
}

func WithCache(c cache.Cache) Option {
	return func(r Reconciler) {
		r.WithCache(c)
	}
}

func WithTarget(t *gnmictarget.Target) Option {
	return func(r Reconciler) {
		r.WithTarget(t)
	}
}

// reconciler defines the parameters for the collector
type reconciler struct {
	nsTargetName string
	targetName   string
	namespace    string
	gnmicTarget  *gnmictarget.Target
	target       target.Target
	cache        cache.Cache
	ctx          context.Context

	stopCh chan bool // used to stop the child go routines if the target gets deleted

	log logging.Logger
}

// NewCollector creates a new GNMI collector
func New(t *types.TargetConfig, namespace string, opts ...Option) (Reconciler, error) {
	r := &reconciler{
		namespace: namespace,
		stopCh:    make(chan bool),
		ctx:       context.Background(),
	}
	for _, opt := range opts {
		opt(r)
	}
	r.nsTargetName = r.gnmicTarget.Config.Name
	r.targetName = r.gnmicTarget.Config.Name

	r.gnmicTarget = gnmictarget.NewTarget(t)
	if err := r.gnmicTarget.CreateGNMIClient(r.ctx, grpc.WithBlock()); err != nil { // TODO add dialopts
		return nil, errors.Wrap(err, errCreateGnmiClient)
	}

	return r, nil
}

func (r *reconciler) WithLogger(log logging.Logger) {
	r.log = log
}

func (r *reconciler) WithCache(tc cache.Cache) {
	r.cache = tc
}

func (r *reconciler) WithTarget(t *gnmictarget.Target) {
	r.gnmicTarget = t
}

// Stop reconciler
func (r *reconciler) Stop() error {
	log := r.log.WithValues("target", r.gnmicTarget.Config.Name, "address", r.gnmicTarget.Config.Address)
	log.Debug("stop target reconciler...")

	r.stopCh <- true

	return nil
}

// Start reconciler
func (r *reconciler) Start() error {
	log := r.log.WithValues("target", r.gnmicTarget.Config.Name, "address", r.gnmicTarget.Config.Address)
	log.Debug("starting target reconciler...")

	errChannel := make(chan error)
	go func() {
		if err := r.run(); err != nil {
			errChannel <- errors.Wrap(err, "error starting target reconciler")
		}
		errChannel <- nil
	}()
	return nil
}

// run reconciler
func (r *reconciler) run() error {
	log := r.log.WithValues("target", r.gnmicTarget.Config.Name, "address", r.gnmicTarget.Config.Address)
	log.Debug("running target reconciler...")

	timeout := make(chan bool, 1)
	timeout <- true

	systemCacheNsTargetName := meta.NamespacedName(r.gnmicTarget.Config.Name).GetPrefixNamespacedName(origin.System)
	ce, err := r.cache.GetEntry(systemCacheNsTargetName)
	if err != nil {
		log.Debug("cannot get cache entry", "error", err)
		return err
	}

	// set cache status to up
	log.Debug("targetreconciler run", "systemnsTargetName", systemCacheNsTargetName, "runningConfig", ce.GetRunningConfig())
	if err := ce.SetSystemExhausted(0); err != nil {
		return err
	}
	for {
		select {
		case <-timeout:
			time.Sleep(reconcileTimer)
			timeout <- true

			// reconcile cache when:
			// -> target is not exhausted
			// -> new updates from k8s operator are received
			// else dont do anything since we need to wait for an update

			exhausted, err := ce.GetSystemExhausted()
			if err != nil {
				log.Debug("error getting exhausted", "error", err)
			} else {
				if *exhausted == 0 {
					if err := r.handlePendingResources(); err != nil {
						r.log.Debug("reconciler", "error", err)
					}

				} else {
					*exhausted--
					ce.SetSystemExhausted(*exhausted)
				}
			}

		case <-r.stopCh:
			log.Debug("Stopping target reconciler")
			return nil
		}
	}
}
