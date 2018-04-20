// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xilinx

import (
	"fmt"
	"net"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

type pluginServiceV1Beta1 struct {
	ngm *xilinxFPGAManager
}

func (s *pluginServiceV1Beta1) GetDevicePluginOptions(ctx context.Context, e *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (s *pluginServiceV1Beta1) ListAndWatch(emtpy *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	glog.Infoln("device-plugin: ListAndWatch start")
	changed := true
	for {
		if changed {
			resp := new(pluginapi.ListAndWatchResponse)
			for _, dev := range s.ngm.devices {
				resp.Devices = append(resp.Devices, &pluginapi.Device{ID: dev.ID, Health: dev.Health})
			}
			glog.Infof("ListAndWatch: send devices %v\n", resp)
			if err := stream.Send(resp); err != nil {
				glog.Errorf("device-plugin: cannot update device states: %v\n", err)
				s.ngm.grpcServer.Stop()
				return err
			}
		}
		time.Sleep(5 * time.Second)
		changed = s.ngm.CheckDeviceStates()
	}
}

func (s *pluginServiceV1Beta1) Allocate(ctx context.Context, requests *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resps := new(pluginapi.AllocateResponse)
	for _, rqt := range requests.ContainerRequests {
		resp := new(pluginapi.ContainerAllocateResponse)
		// Add all requested devices to Allocate Response
		for _, id := range rqt.DevicesIDs {
			dev, ok := s.ngm.devices[id]
			if !ok {
				return nil, fmt.Errorf("invalid allocation request with non-existing device %s", id)
			}
			if dev.Health != pluginapi.Healthy {
				return nil, fmt.Errorf("invalid allocation request with unhealthy device %s", id)
			}
			go s.allocateDevice(id, image)
			resp.Devices = append(resp.Devices, &pluginapi.DeviceSpec{
				HostPath:      pciDevicesPath + "/" + id,
				ContainerPath: pciDevicesPath + "/" + id,
				Permissions:   "mrw",
			})
		}
		// Add all default devices to Allocate Response
		for _, d := range s.ngm.defaultDevices {
			resp.Devices = append(resp.Devices, &pluginapi.DeviceSpec{
				HostPath:      d,
				ContainerPath: d,
				Permissions:   "mrw",
			})
		}

		// resp.Mounts = append(resp.Mounts, &pluginapi.Mount{
		// 	ContainerPath: s.ngm.containerPathPrefix,
		// 	HostPath:      s.ngm.hostPathPrefix,
		// 	ReadOnly:      true,
		// })
		resp.Envs = map[string]string{
			"LD_LIBRARY_PATH": fmt.Sprintf("%s/lib:${LD_LIBRARY_PATH}", s.ngm.containerPathPrefix),
		}
		resps.ContainerResponses = append(resps.ContainerResponses, resp)
	}
	return resps, nil
}

func (s *pluginServiceV1Beta1) PreStartContainer(ctx context.Context, r *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	glog.Errorf("device-plugin: PreStart should NOT be called for xilinx FPGA device plugin\n")
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (s *pluginServiceV1Beta1) RegisterService() {
	pluginapi.RegisterDevicePluginServer(s.ngm.grpcServer, s)
}

// TODO: remove this function once we move to probe based registration.
func RegisterWithV1Beta1Kubelet(kubeletEndpoint, pluginEndpoint, resourceName string) error {
	conn, err := grpc.Dial(kubeletEndpoint, grpc.WithInsecure(),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	if err != nil {
		return fmt.Errorf("device-plugin: cannot connect to kubelet service: %v", err)
	}
	defer conn.Close()
	client := pluginapi.NewRegistrationClient(conn)

	request := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     pluginEndpoint,
		ResourceName: resourceName,
	}

	if _, err = client.Register(context.Background(), request); err != nil {
		return fmt.Errorf("device-plugin: cannot register to kubelet service: %v", err)
	}
	return nil
}

func (s *pluginServiceV1Beta1) allocateDevice(id, image string) error {
	glog.Infoln("device-plugin: allocate device")

	_, err := ExecCommand("/usr/local/bin/FpgaCmdEntry", "LF", "-S", "0", "-I", image)
	return err
}
