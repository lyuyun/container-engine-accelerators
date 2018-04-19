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
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"google.golang.org/grpc"

	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

const (
	pciDevFmt = "%04x:%02x:%02x.%d"
	pciDevResourcePath = "/sys/bus/pci/devices/%s/resource%u"
	pciDevResourceWcPath = "/sys/bus/pci/devices/%s/resource%u_wc"
	pciDevicesPath = "/sys/bus/pci/devices"
	pciDevVendorPath = "/sys/bus/pci/devices/%s/vendor"
	pciDevDevicePath = "/sys/bus/pci/devices/%s/device"

	hwDPDKVFVendorID = "0x19e5"
	hwDPDKVFDeviceID = "0xd503"
	hwOCLPFVendorID = "0x19e5"
	hwOCLPFDeviceID = "0xd512"
)

var (
	resourceName = "xilinx.com/fpga"
)

// xilinxFPGAManager manages xilinx fpga devices.
type xilinxFPGAManager struct {
	hostPathPrefix      string
	containerPathPrefix string
	defaultDevices      []string
	devices             map[string]pluginapi.Device
	grpcServer          *grpc.Server
	socket              string
	stop                chan bool
}

func NewXilinxFPGAManager(hostPathPrefix, containerPathPrefix string) *xilinxFPGAManager {
	return &xilinxFPGAManager{
		hostPathPrefix:      hostPathPrefix,
		containerPathPrefix: containerPathPrefix,
		devices:             make(map[string]pluginapi.Device),
		stop:                make(chan bool),
	}
}

// Discovers all XILINX FPGA devices available on the local node by walking `/dev` directory.
func (ngm *xilinxFPGAManager) discoverFPGAs() error {
	files, err := ioutil.ReadDir(pciDevicesPath)
	if err != nil {
		return err
	}

	for _, f := range files {
		var domain, bus, device, function int
		dirName := f.Name()
		n, err := fmt.Sscanf(dirName, pciDevFmt, &domain, &bus, &device, &function)
		if err != nil || n != 4 {
			continue
		}
		vendorID, err := getFileData(pciDevVendorPath, dirName)
		if err != nil {
			continue
		}
		deviceID, err := getFileData(pciDevDevicePath, dirName)
		if err != nil {
			continue
		}
		if (vendorID == hwDPDKVFVendorID && deviceID == hwDPDKVFDeviceID) ||
			(vendorID == hwOCLPFVendorID && deviceID == hwOCLPFDeviceID) {
			glog.Infof("Found Xilinx FPGA %q, VendorID %q, DeviceID %q\n", dirName, vendorID, deviceID)
			ngm.devices[f.Name()] = pluginapi.Device{ID: dirName, Health: pluginapi.Healthy}
		}
	}

	if len(ngm.defaultDevices) == 0 && len(ngm.devices) == 0 {
		return fmt.Errorf("No Xilinx FPGA device found")
	}

	return nil
}

func (ngm *xilinxFPGAManager) GetDeviceState(DeviceName string) string {
	// TODO: calling Xilinx tools to figure out actual device state
	return pluginapi.Healthy
}

// Discovers Xilinx FPGA devices and sets up device access environment.
func (ngm *xilinxFPGAManager) Start() error {
	if err := ngm.discoverFPGAs(); err != nil {
		return err
	}

	return nil
}

func (ngm *xilinxFPGAManager) CheckDeviceStates() bool {
	changed := false
	for id, dev := range ngm.devices {
		state := ngm.GetDeviceState(id)
		if dev.Health != state {
			changed = true
			dev.Health = state
			ngm.devices[id] = dev
		}
	}
	return changed
}

func (ngm *xilinxFPGAManager) Serve(pMountPath, kEndpoint, pluginEndpoint string) {
	registerWithKubelet := false
	if _, err := os.Stat(path.Join(pMountPath, kEndpoint)); err == nil {
		glog.Infof("will use alpha API\n")
		registerWithKubelet = true
	} else {
		glog.Infof("will use beta API\n")
	}

	for {
		select {
		case <-ngm.stop:
			close(ngm.stop)
			return
		default:
			{
				pluginEndpointPath := path.Join(pMountPath, pluginEndpoint)
				glog.Infof("starting device-plugin server at: %s\n", pluginEndpointPath)
				lis, err := net.Listen("unix", pluginEndpointPath)
				if err != nil {
					glog.Fatalf("starting device-plugin server failed: %v", err)
				}
				ngm.socket = pluginEndpointPath
				ngm.grpcServer = grpc.NewServer()

				// Registers the supported versions of service.
				pluginalpha := &pluginServiceV1Alpha{ngm: ngm}
				pluginalpha.RegisterService()
				pluginbeta := &pluginServiceV1Beta1{ngm: ngm}
				pluginbeta.RegisterService()

				var wg sync.WaitGroup
				wg.Add(1)
				// Starts device plugin service.
				go func() {
					defer wg.Done()
					// Blocking call to accept incoming connections.
					err := ngm.grpcServer.Serve(lis)
					glog.Errorf("device-plugin server stopped serving: %v", err)
				}()

				if registerWithKubelet {
					// Wait till the grpcServer is ready to serve services.
					for len(ngm.grpcServer.GetServiceInfo()) <= 0 {
						time.Sleep(1 * time.Second)
					}
					glog.Infoln("device-plugin server started serving")
					// Registers with Kubelet.
					err = RegisterWithKubelet(path.Join(pMountPath, kEndpoint), pluginEndpoint, resourceName)
					if err != nil {
						glog.Infof("falling back to v1beta1 API: %v\n", err)
						err = RegisterWithV1Beta1Kubelet(path.Join(pMountPath, kEndpoint), pluginEndpoint, resourceName)
					}
					if err != nil {
						ngm.grpcServer.Stop()
						wg.Wait()
						glog.Fatal(err)
					}
					glog.Infoln("device-plugin registered with the kubelet")
				}

				// This is checking if the plugin socket was deleted. If so,
				// stop the grpc server and start the whole thing again.
				for {
					if _, err := os.Lstat(pluginEndpointPath); err != nil {
						glog.Infof("stopping device-plugin server at: %s\n", pluginEndpointPath)
						glog.Errorln(err)
						ngm.grpcServer.Stop()
						break
					}
					time.Sleep(1 * time.Second)
				}
				wg.Wait()
			}
		}
	}
}

func (ngm *xilinxFPGAManager) Stop() error {
	glog.Infof("removing device plugin socket %s\n", ngm.socket)
	if err := os.Remove(ngm.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	ngm.stop <- true
	<-ngm.stop
	return nil
}

func getFileData(filePath, fileName string) (string, error) {
	file := fmt.Sprintf(filePath, fileName)

	data, err := ioutil.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("failed to read the file %q: %v", file, err)
	}

	if len(data) == 0 {
		return "", fmt.Errorf("no data in the file %q", file)
	}

	return strings.TrimSpace(string(data)), nil
}
