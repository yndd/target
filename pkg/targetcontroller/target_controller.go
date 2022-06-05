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

	"github.com/yndd/cache/pkg/cache"
	"github.com/yndd/grpcserver"
	pkgmetav1 "github.com/yndd/ndd-core/apis/pkg/meta/v1"
	pkgv1 "github.com/yndd/ndd-core/apis/pkg/v1"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/targetchannel"
	"github.com/yndd/registrator/registrator"
)

type TargetController interface {
	// start the target controller
	Start() error
	// stop the target controller
	Stop() error
	// GetTargetChannel
	GetTargetChannel() chan targetchannel.TargetMsg
	// SetStartTargetHandler
	SetStartTargetHandler(h TargetHandler)
	// SetStopTargetHandler
	SetStopTargetHandler(h TargetHandler)
}

type Options struct {
	Logger      logging.Logger
	GrpcServer  *grpcserver.GrpcServer
	Registrator registrator.Registrator
	Cache       cache.Cache
}

type TargetHandler func(nsTargetName string)

// Option can be used to manipulate Collector config.
type Option func(TargetController)

func SetStartTargetHandler(h TargetHandler) Option {
	return func(t TargetController) {
		t.SetStartTargetHandler(h)
	}
}

func SetStopTargetHandler(h TargetHandler) Option {
	return func(t TargetController) {
		t.SetStopTargetHandler(h)
	}
}

// targetController implements the TargetController interface
type targetController struct {
	options *Options
	//handlers
	startTargetHandler TargetHandler
	stopTargetHandler  TargetHandler

	// channels
	targetCh chan targetchannel.TargetMsg
	stopCh   chan bool

	// server
	//server *grpcserver.GrpcServer
	// registrator
	registrator registrator.Registrator

	ctx context.Context
	log logging.Logger
}

func New(ctx context.Context, o *Options, opts ...Option) TargetController {
	log := o.Logger
	log.Debug("new target controller")

	c := &targetController{
		log:         o.Logger,
		options:     o, // contains all options
		targetCh:    make(chan targetchannel.TargetMsg),
		stopCh:      make(chan bool),
		registrator: o.Registrator,
		ctx:         ctx,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *targetController) GetTargetChannel() chan targetchannel.TargetMsg {
	return c.targetCh
}

func (c *targetController) SetStartTargetHandler(h TargetHandler) {
	c.startTargetHandler = h
}

func (c *targetController) SetStopTargetHandler(h TargetHandler) {
	c.stopTargetHandler = h
}

func (c *targetController) Stop() error {
	c.log.Debug("stopping target controller...")

	close(c.stopCh)
	return nil
}

func (c *targetController) Start() error {
	c.log.Debug("starting target controller...")

	var err error
	go func() error {
		err := c.options.GrpcServer.Start(context.Background())
		if err != nil {
			return err
		}
		return nil
	}()
	if err != nil {
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
		HealthChecks: []registrator.HealthKind{registrator.HealthKindGRPC, registrator.HealthKindTTL},
	})

	// start target handler, which enables crud operations for targets
	// create, delete requests
	go c.startTargetWorker(c.ctx)

	return nil
}

func (c *targetController) startTargetWorker(ctx context.Context) {
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
			//log := c.log.WithValues("targetFullName", t.Target)
			switch t.Operation {
			case targetchannel.Start:
				// call start target handler
				c.startTargetHandler(t.Target)
			case targetchannel.Stop:
				// call stop target handler
				c.stopTargetHandler(t.Target)
			}
		}
	}
}
