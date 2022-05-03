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

package targetcontroller

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/ndd-target-runtime/internal/cache"
	"github.com/yndd/ndd-target-runtime/pkg/cachename"
	"github.com/yndd/ndd-target-runtime/internal/gnmiserver"
	"github.com/yndd/ndd-target-runtime/pkg/resource"
	"github.com/yndd/ndd-target-runtime/pkg/target"
	"github.com/yndd/ndd-target-runtime/internal/targetchannel"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// errors
	errCreateGnmiClient     = "cannot create gnmi client"
	errTargetInitFailed     = "cannot initialize the target"
	errTargetiscoveryFailed = "cannot discover target"
)

// Option can be used to manipulate TargetController config.
type Option func(TargetController)

// TargetController defines the interfaces for the target controller
type TargetController interface {
	//options
	// add a logger to TargetController
	WithLogger(log logging.Logger)
	// add a k8s client to TargetController
	WithClient(c resource.ClientApplicator)
	// add an event channel to TargetController
	WithEventCh(eventChs map[string]chan event.GenericEvent)
	// add the target registry to TargetController
	WithTargetsRegistry(target.TargetRegistry)
	// add the target model to the TargetController
	WithTargetModel(*model.Model)
	//targetinstance methods
	// add a target instance to the target controller
	AddTargetInstance(targetName string, t TargetInstance)
	// delete a target instance from the target controller
	DeleteTargetInstance(targetName string) error
	// get a target instance from the target controller
	GetTargetInstance(targetName string) TargetInstance
	// target controller methods
	// start the target controller
	Start() error
	// stop the target controller
	Stop() error
}

// WithLogger adds a logger to the target controller
func WithLogger(l logging.Logger) Option {
	return func(o TargetController) {
		o.WithLogger(l)
	}
}

// WithClient adds a k8s client to the target controller.
func WithClient(c resource.ClientApplicator) Option {
	return func(o TargetController) {
		o.WithClient(c)
	}
}

// WithEventCh adds the event channel to the Target Controller
func WithEventCh(eventChs map[string]chan event.GenericEvent) Option {
	return func(o TargetController) {
		o.WithEventCh(eventChs)
	}
}

// WithClient adds a target registry to the Target Controller
func WithTargetsRegistry(t target.TargetRegistry) Option {
	return func(o TargetController) {
		o.WithTargetsRegistry(t)
	}
}

// WithTargetModel adds the target model to the target Controller
func WithTargetModel(m *model.Model) Option {
	return func(o TargetController) {
		o.WithTargetModel(m)
	}
}

// targetControllerImpl implements the TargetController interface
type targetControllerImpl struct {
	// gnmi target keeps track of the inidividual target info
	// and processes
	m              sync.RWMutex
	targets        map[string]TargetInstance
	cache          cache.Cache
	targetRegistry target.TargetRegistry
	targetModel    *model.Model

	// channels
	targetCh chan targetchannel.TargetMsg
	stopCh   chan bool

	// kubernetes
	client   resource.ClientApplicator
	eventChs map[string]chan event.GenericEvent
	// server
	server gnmiserver.GnmiServer

	ctx context.Context
	cfn context.CancelFunc
	log logging.Logger
}

func New(ctx context.Context, opts ...Option) TargetController {
	c := &targetControllerImpl{
		m:        sync.RWMutex{},
		targets:  make(map[string]TargetInstance),
		targetCh: make(chan targetchannel.TargetMsg),
		stopCh:   make(chan bool),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.ctx, c.cfn = context.WithCancel(ctx)

	// initialize the multi-device cache
	c.cache = cache.New()

	return c
}

func (c *targetControllerImpl) WithLogger(l logging.Logger) {
	c.log = l
}

func (c *targetControllerImpl) WithClient(rc resource.ClientApplicator) {
	c.client = rc
}

func (c *targetControllerImpl) WithEventCh(eventChs map[string]chan event.GenericEvent) {
	c.eventChs = eventChs
}

func (c *targetControllerImpl) WithTargetsRegistry(t target.TargetRegistry) {
	c.targetRegistry = t
}

func (c *targetControllerImpl) WithTargetModel(m *model.Model) {
	c.targetModel = m
}

func (c *targetControllerImpl) GetTargetInstance(targetName string) TargetInstance {
	c.m.Lock()
	defer c.m.Unlock()
	t, ok := c.targets[targetName]
	if !ok {
		return nil
	}
	return t
}

func (c *targetControllerImpl) AddTargetInstance(nsTargetName string, t TargetInstance) {
	c.m.Lock()
	defer c.m.Unlock()
	c.targets[nsTargetName] = t
}

func (c *targetControllerImpl) DeleteTargetInstance(nsTargetName string) error {
	c.m.Lock()
	defer c.m.Unlock()
	if ti, ok := c.targets[nsTargetName]; ok {
		if err := ti.StopTargetCollector(); err != nil {
			return err
		}
		if err := ti.StopTargetReconciler(); err != nil {
			return err
		}
	}
	delete(c.targets, nsTargetName)
	targetCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.TargetCachePrefix)
	c.cache.DeleteEntry(targetCacheNsTargetName)
	return nil
}

func (c *targetControllerImpl) Stop() error {
	c.log.Debug("stopping  deviceDriver...")

	close(c.stopCh)
	return nil
}

func (c *targetControllerImpl) Start() error {
	c.log.Debug("starting targetdriver...")

	// start gnmi server
	c.server = gnmiserver.New(
		gnmiserver.WithCache(c.cache),
		gnmiserver.WithLogger(c.log),
		gnmiserver.WithTargetChannel(c.targetCh),
	)
	if err := c.server.Start(); err != nil {
		return err
	}

	// start target handler, which enables crud operations for targets
	// create, delete requests
	go c.startTargetWorker(c.ctx)

	return nil
}

func (c *targetControllerImpl) startTargetWorker(ctx context.Context) {
	c.log.Debug("Starting targetChangeHandler...")

	for {
		select {
		case <-ctx.Done():
			c.log.Debug("target controller worker stopped", "error", ctx.Err())
			return
		case <-c.stopCh:
			c.log.Debug("target controller worker stopped")
			return
		case t := <-c.targetCh:
			log := c.log.WithValues("targetFullName", t.Target)
			switch t.Operation {
			case targetchannel.Start:
				if err := c.startTarget(t.Target); err != nil {
					log.Debug("target init/start failed", "error", err)

					// delete the context since it is not ok to connect to the device
					if err := c.DeleteTargetInstance(t.Target); err != nil {
						log.Debug("target delete failed", "error", err)
					}
				} else {
					log.Debug("target init/start success")
				}
			case targetchannel.Stop:
				// stop the target and delete the target from the targetlist
				if err := c.stopTarget(t.Target); err != nil {
					log.Debug("target stop failed", "error", err)
					if err := c.DeleteTargetInstance(t.Target); err != nil {
						log.Debug("target delete failed", "error", err)
					}
				} else {
					c.log.Debug("target stop success")
					if err := c.DeleteTargetInstance(t.Target); err != nil {
						c.log.Debug("target delete failed", "error", err)
					}
				}
			}
		}
	}
}

func (c *targetControllerImpl) startTarget(nsTargetName string) error {
	log := c.log.WithValues("nsTargetName", nsTargetName)
	log.Debug("start target...")
	// the target we get on the channel has <namespace.target> semantics
	targetName := cachename.NamespacedName(nsTargetName).GetName()
	namespace := cachename.NamespacedName(nsTargetName).GetNameSpace()

	targetCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.TargetCachePrefix)
	if !c.cache.Exists(targetCacheNsTargetName) {
		return fmt.Errorf("target cache not initialized: %s", targetCacheNsTargetName)
	}

	ti, err := NewTargetInstance(c.ctx, namespace, nsTargetName, targetName,
		WithTargetInstanceCache(c.cache),
		WithTargetInstanceClient(c.client),
		WithTargetInstanceLogger(c.log),
		WithTargetInstanceEventCh(c.eventChs),
		WithTargetInstanceTargetsRegistry(c.targetRegistry),
	)
	if err != nil {
		return err
	}
	c.AddTargetInstance(nsTargetName, ti)

	// get capabilities the device
	cap, err := ti.GetCapabilities()
	if err != nil {
		return err
	}
	enc := target.GetSupportedEncodings(cap)
	// discover the target
	if err := ti.Discover(enc); err != nil {
		return err
	}
	// update model data from capabilities to the targetmodel
	c.targetModel.ModelData = model.GetModelData(cap.SupportedModels)

	// initialize the config target cache
	configCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.ConfigCachePrefix)
	cce := cache.NewCacheEntry(configCacheNsTargetName)
	cce.SetModel(c.targetModel)
	c.cache.AddEntry(cce)

	// initialize the system target cache
	systemCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.SystemCachePrefix)
	sce := cache.NewCacheEntry(systemCacheNsTargetName)
	sce.SetModel(&model.Model{
		ModelData:       []*gnmi.ModelData{},
		StructRootType:  reflect.TypeOf((*ygotnddp.Device)(nil)),
		SchemaTreeRoot:  ygotnddp.SchemaTree["Device"],
		JsonUnmarshaler: ygotnddp.Unmarshal,
		EnumData:        ygotnddp.ΛEnum,
	})
	c.cache.AddEntry(sce)

	if err := ti.GetInitialTargetConfig(); err != nil {
		c.log.Debug("initialize target config", "error", err)
		return errors.Wrap(err, "cannot get or initialize initial config")
	}

	if err := ti.InitializeSystemConfig(); err != nil {
		c.log.Debug("initialize system config", "error", err)
		return errors.Wrap(err, "cannot validate system config")
	}

	if err := ti.StartTargetReconciler(); err != nil {
		c.log.Debug("start target reconciler", "error", err)
		return errors.Wrap(err, "cannot start target reconciler")
	}

	if err := ti.StartTargetCollector(); err != nil {
		c.log.Debug("start target colelctor", "error", err)
		return errors.Wrap(err, "cannot start target collector")
	}

	return nil
}

func (c *targetControllerImpl) stopTarget(nsTargetName string) error {
	// delete the target instance -> stops the collectors, reconciler
	c.DeleteTargetInstance(nsTargetName)
	// clear the cache from the device
	targetCacheNsTargetName := cachename.NamespacedName(nsTargetName).GetPrefixNamespacedName(cachename.TargetCachePrefix)
	c.cache.DeleteEntry(targetCacheNsTargetName)
	return nil
}
