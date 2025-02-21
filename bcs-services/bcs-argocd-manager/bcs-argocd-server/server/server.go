/*
 * Tencent is pleased to support the open source community by making Blueking Container Service available.
 * Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under,
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package server

import (
	"context"
	"crypto/tls"
	"github.com/Tencent/bk-bcs/bcs-common/common/blog"
	"github.com/Tencent/bk-bcs/bcs-common/common/ssl"
	"github.com/Tencent/bk-bcs/bcs-common/common/static"
	"github.com/Tencent/bk-bcs/bcs-common/common/version"
	"github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/bcs-argocd-server/internal/common"
	discovery "github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/bcs-argocd-server/internal/dicsovery"
	"github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/bcs-argocd-server/internal/handler"
	"github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/bcs-argocd-server/internal/options"
	"github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/bcs-argocd-server/internal/utils"
	"github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/pkg/sdk/instance"
	"github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/pkg/sdk/plugin"
	"github.com/Tencent/bk-bcs/bcs-services/bcs-argocd-manager/pkg/sdk/project"
	gClient "github.com/asim/go-micro/plugins/client/grpc/v4"
	microEtcd "github.com/asim/go-micro/plugins/registry/etcd/v4"
	gServer "github.com/asim/go-micro/plugins/server/grpc/v4"
	"go-micro.dev/v4"
	"strconv"
	"time"

	microRgt "go-micro.dev/v4/registry"
)

// ArgocdServer is the main server struct
type ArgocdServer struct {
	opt *options.ArgocdServerOptions

	microSvc  micro.Service
	microRtr  microRgt.Registry
	discovery *discovery.ModuleDiscovery

	// http service
	//httpServer *http.Server

	// metric service
	//metricServer *http.Server

	// tls config for bcs argocd server service and client side
	tlsConfig       *tls.Config
	clientTLSConfig *tls.Config

	ctx           context.Context
	ctxCancelFunc context.CancelFunc
	stopCh        chan struct{}
}

// NewArgocdServer create a new bcs argocd server
func NewArgocdServer(opt *options.ArgocdServerOptions) *ArgocdServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ArgocdServer{
		opt:           opt,
		ctx:           ctx,
		ctxCancelFunc: cancel,
		stopCh:        make(chan struct{}),
	}
}

// Init bcs argocd server
func (as *ArgocdServer) Init() error {
	for _, f := range []func() error{
		as.initTLSConfig,
		as.initRegistry,
		as.initDiscovery,
		as.initMicro,
		//as.initHTTPService,
		//as.initMetric,
	} {
		if err := f(); err != nil {
			return err
		}
	}

	return nil
}

// Run bcs argocd server
func (as *ArgocdServer) Run() error {
	// run the service
	if err := as.microSvc.Run(); err != nil {
		blog.Fatal(err)
	}
	blog.CloseLogs()
	return nil
}

// init server and client tls config
func (as *ArgocdServer) initTLSConfig() error {
	if len(as.opt.ServerCert) != 0 && len(as.opt.ServerKey) != 0 && len(as.opt.ServerCa) != 0 {
		tlsConfig, err := ssl.ServerTslConfVerityClient(as.opt.ServerCa, as.opt.ServerCert,
			as.opt.ServerKey, static.ServerCertPwd)
		if err != nil {
			blog.Errorf("load bcs argocd server tls config failed, err %s", err.Error())
			return err
		}
		as.tlsConfig = tlsConfig
		blog.Infof("load bcs argocd server tls config successfully")
	}

	if len(as.opt.ClientCert) != 0 && len(as.opt.ClientKey) != 0 && len(as.opt.ClientCa) != 0 {
		tlsConfig, err := ssl.ClientTslConfVerity(as.opt.ClientCa, as.opt.ClientCert,
			as.opt.ClientKey, static.ClientCertPwd)
		if err != nil {
			blog.Errorf("load bcs argocd server client tls config failed, err %s", err.Error())
			return err
		}
		as.clientTLSConfig = tlsConfig
		blog.Infof("load bcs argocd server client tls config successfully")
	}
	return nil
}

func (as *ArgocdServer) initRegistry() error {
	etcdEndpoints := utils.SplitAddrString(as.opt.Etcd.EtcdEndpoints)
	etcdSecure := false

	var etcdTLS *tls.Config
	var err error
	if len(as.opt.Etcd.EtcdCa) != 0 && len(as.opt.Etcd.EtcdCert) != 0 && len(as.opt.Etcd.EtcdKey) != 0 {
		etcdSecure = true
		etcdTLS, err = ssl.ClientTslConfVerity(as.opt.Etcd.EtcdCa, as.opt.Etcd.EtcdCert, as.opt.Etcd.EtcdKey, "")
		if err != nil {
			return err
		}
	}

	blog.Infof("get etcd endpoints for registry: %v, with secure %t", etcdEndpoints, etcdSecure)

	as.microRtr = microEtcd.NewRegistry(
		microRgt.Addrs(etcdEndpoints...),
		microRgt.Secure(etcdSecure),
		microRgt.TLSConfig(etcdTLS),
	)
	if err := as.microRtr.Init(); err != nil {
		return err
	}
	return nil
}

func (as *ArgocdServer) initDiscovery() error {
	as.discovery = discovery.NewModuleDiscovery(common.ServiceDomain, as.microRtr)
	blog.Infof("init discovery for bcs argocd server successfully")
	return nil
}

func (as *ArgocdServer) initMicro() error {
	svc := micro.NewService(
		micro.Client(gClient.NewClient(gClient.AuthTLS(as.tlsConfig))),
		micro.Server(gServer.NewServer(gServer.AuthTLS(as.tlsConfig))),
		micro.Name(common.ServiceDomain),
		micro.Metadata(map[string]string{
			common.MicroMetaKeyHTTPPort: strconv.Itoa(int(as.opt.HTTPPort)),
		}),
		micro.Address(as.opt.Address+":"+strconv.Itoa(int(as.opt.Port))),
		micro.Registry(as.microRtr),
		micro.Version(version.BcsVersion),
		micro.RegisterTTL(30*time.Second),
		micro.RegisterInterval(25*time.Second),
		micro.Context(as.ctx),
		micro.BeforeStart(func() error {
			return nil
		}),
		micro.AfterStart(func() error {
			return as.discovery.Start()
		}),
		micro.BeforeStop(func() error {
			as.discovery.Stop()
			return nil
		}),
	)

	if err := instance.RegisterInstanceServiceHandler(svc.Server(), handler.NewInstanceHandler()); err != nil {
		blog.Errorf("register bcs argocd instance handler to micro failed: %s", err.Error())
		return nil
	}

	if err := project.RegisterProjectServiceHandler(svc.Server(), handler.NewProjectHandler()); err != nil {
		blog.Errorf("register bcs argocd project handler to micro failed: %s", err.Error())
		return nil
	}

	if err := plugin.RegisterPluginServiceHandler(svc.Server(), handler.NewPluginHandler()); err != nil {
		blog.Errorf("register bcs argocd plugin handler to micro failed: %s", err.Error())
		return nil
	}

	as.microSvc = svc
	blog.Infof("success to register bcs argocd server handlers to micro")
	return nil
}
