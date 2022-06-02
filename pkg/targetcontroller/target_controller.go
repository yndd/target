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
	"os"
	"sync"

	pkgmetav1 "github.com/yndd/ndd-core/apis/pkg/meta/v1"
	pkgv1 "github.com/yndd/ndd-core/apis/pkg/v1"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/ndd-runtime/pkg/resource"
	"github.com/yndd/ndd-runtime/pkg/targetchannel"
	"github.com/yndd/registrator/registrator"
	targetv1 "github.com/yndd/target/apis/target/v1"
	"github.com/yndd/target/internal/cache"
	"github.com/yndd/target/pkg/grpcserver"
	"github.com/yndd/target/pkg/target"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// errors
	errCreateGnmiClient     = "cannot create gnmi client"
	errTargetInitFailed     = "cannot initialize the target"
	errTargetiscoveryFailed = "cannot discover target"
)

// TargetController defines the interfaces for the target controller
type TargetController interface {
	//targetinstance methods
	AddStartTargetHandler(StartTargetHandler)
	AddStopTargetHandler(StopTargetHandler)
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
	// GetTargetChannel
	GetTargetChannel() chan targetchannel.TargetMsg
}

type Options struct {
	Logger            logging.Logger
	GrpcServerAddress string
	Registrator       registrator.Registrator
	TargetRegistry    target.TargetRegistry
	TargetModel       *model.Model
	VendorType        targetv1.VendorType
}

// targetControllerImpl implements the TargetController interface
type targetControllerImpl struct {
	options *Options
	m       sync.RWMutex
	targets map[string]TargetInstance
	cache   cache.Cache
	//targetRegistry target.TargetRegistry
	//targetModel *model.Model
	//grpcServerAddress string

	startTargetHandler StartTargetHandler
	stopTargetHandler  StopTargetHandler

	// channels
	targetCh chan targetchannel.TargetMsg
	stopCh   chan bool

	// kubernetes
	client   resource.ClientApplicator          // used to get the target credentials
	eventChs map[string]chan event.GenericEvent // TODO to change to a generic gnmi subscription mechanism
	// server
	server grpcserver.GrpcServer
	// registrator
	registrator registrator.Registrator

	ctx context.Context
	//cfn context.CancelFunc
	log logging.Logger
}

type StartTargetHandler func(nsTargetName string)
type StopTargetHandler func(nsTargetName string)

// Option can be used to manipulate Collector config.
type Option func(TargetController)

// WithLogger specifies how the collector logs messages.
/*
func WithStartTargetHandler(h StartTargetHandler) Option {
	return func(t TargetController) {
		t.WithStartTargetHandler(h)
	}
}
*/

func New(ctx context.Context, config *rest.Config, o *Options, opts ...Option) (TargetController, error) {
	log := o.Logger
	log.Debug("new target controller")

	c := &targetControllerImpl{
		log:         o.Logger,
		options:     o, // contains all options
		m:           sync.RWMutex{},
		targets:     make(map[string]TargetInstance),
		targetCh:    make(chan targetchannel.TargetMsg),
		stopCh:      make(chan bool),
		registrator: o.Registrator,
		cache:       cache.New(), // initialize the multi-device cache
		ctx:         ctx,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

/*
func (c *targetControllerImpl) WithStartTargetHandler(h StartTargetHandler) {
	c.startTargetHandler = h
}

func (c *targetControllerImpl) WithStopTargetHandler(h StopTargetHandler) {
	c.stopTargetHandler = h
}
*/

func (c *targetControllerImpl) AddStartTargetHandler(h StartTargetHandler) {
	c.startTargetHandler = h
}

func (c *targetControllerImpl) AddStopTargetHandler(h StopTargetHandler) {
	c.stopTargetHandler = h
}

func (c *targetControllerImpl) GetTargetChannel() chan targetchannel.TargetMsg {
	return c.targetCh
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
		ti.DeRegister()
	}
	delete(c.targets, nsTargetName)
	return nil
}

func (c *targetControllerImpl) Stop() error {
	c.log.Debug("stopping  deviceDriver...")

	close(c.stopCh)
	return nil
}

func (c *targetControllerImpl) Start() error {
	c.log.Debug("starting targetdriver...")

	// start grpc server
	c.server = grpcserver.New(c.options.GrpcServerAddress,
		grpcserver.WithHealth(true),
		grpcserver.WithGnmi(true),
		grpcserver.WithCache(c.cache),
		grpcserver.WithLogger(c.log),
		grpcserver.WithTargetChannel(c.targetCh),
	)
	if err := c.server.Start(); err != nil {
		return err
	}

	// register the service
	c.registrator.Register(c.ctx, &registrator.Service{
		Name:    os.Getenv("SERVICE_NAME"),
		ID:      os.Getenv("POD_NAME"),
		Port:    pkgmetav1.GnmiServerPort,
		Address: os.Getenv("POD_IP"),
		//Address:    strings.Join([]string{os.Getenv("POD_NAME"), os.Getenv("GRPC_SVC_NAME"), os.Getenv("POD_NAMESPACE"), "svc", "cluster", "local"}, "."),
		Tags:         pkgv1.GetServiceTag(os.Getenv("POD_NAMESPACE"), os.Getenv("POD_NAME")),
		HealthChecks: []registrator.HealthKind{registrator.HealthKindTTL, registrator.HealthKindGRPC},
	})

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
				c.startTargetHandler(t.Target)

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
				c.stopTargetHandler(t.Target)

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
