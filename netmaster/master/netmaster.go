/***
Copyright 2014 Cisco Systems Inc. All rights reserved.

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

package master

import (
	"errors"
	"fmt"

	"github.com/cenkalti/backoff"
	"github.com/contiv/netplugin/core"
	"github.com/contiv/netplugin/netmaster/gstate"
	"github.com/contiv/netplugin/netmaster/intent"
	"github.com/contiv/netplugin/netmaster/mastercfg"
	"github.com/contiv/netplugin/utils"
	"github.com/contiv/netplugin/utils/netutils"

	log "github.com/Sirupsen/logrus"
	"github.com/samalba/dockerclient"
)

const (
	defaultInfraNetName = "infra"
	defaultSkyDNSImage  = "skynetservices/skydns:latest"
)

// Run Time config of netmaster
type nmRunTimeConf struct {
	clusterMode string
	dnsEnabled  bool
}

var masterRTCfg nmRunTimeConf

// SetClusterMode sets the cluster mode for the contiv plugin
func SetClusterMode(cm string) error {
	switch cm {
	case "docker":
	case "kubernetes":
	case "test": // internal mode used for integration testing
		break
	default:
		return core.Errorf("%s not a valid cluster mode {docker | kubernetes}", cm)
	}

	masterRTCfg.clusterMode = cm
	return nil
}

// GetClusterMode gets the cluster mode of the contiv plugin
func GetClusterMode() string {
	return masterRTCfg.clusterMode
}

// IsDNSEnabled gets the status of whether DNS is enabled
func IsDNSEnabled() bool {
	return masterRTCfg.dnsEnabled
}

// SetDNSEnabled sets the status of DNS Enable
func SetDNSEnabled(dnsEnableFlag bool) error {
	log.Infof("Setting dns flag to %v", dnsEnableFlag)
	masterRTCfg.dnsEnabled = dnsEnableFlag
	return nil
}

func getDNSName(tenantName string) string {
	return tenantName + "dns"
}

func getEpName(networkName string, ep *intent.ConfigEP) string {
	if ep.Container != "" {
		return networkName + "-" + ep.Container
	}

	return ep.Host + "-native-intf"
}

func validateTenantConfig(tenant *intent.ConfigTenant) error {
	if tenant.Name == "" {
		return core.Errorf("invalid tenant name")
	}

	if tenant.VLANs != "" {
		if _, err := netutils.ParseTagRanges(tenant.VLANs, "vlan"); err != nil {
			log.Errorf("error parsing vlan range '%s'. Error: %s", tenant.VLANs, err)
			return err
		}
	}

	if tenant.VXLANs != "" {
		if _, err := netutils.ParseTagRanges(tenant.VXLANs, "vxlan"); err != nil {
			log.Errorf("error parsing vxlan range '%s'.Error: %s", tenant.VXLANs, err)
			return err
		}
	}

	return nil
}

// CreateGlobal sets the global state
func CreateGlobal(stateDriver core.StateDriver, gc *intent.ConfigGlobal) error {
	log.Infof("Received global create with intent {%v}", gc)
	var err error
	gcfgUpdateList := []string{}

	masterGc := &mastercfg.GlobConfig{}
	masterGc.StateDriver = stateDriver
	masterGc.Read("global")

	gstate.GlobalMutex.Lock()
	defer gstate.GlobalMutex.Unlock()
	gCfg := &gstate.Cfg{}
	gCfg.StateDriver = stateDriver
	gCfg.Read("global")

	// check for valid values
	if gc.NwInfraType != "" {
		switch gc.NwInfraType {
		case "default", "aci", "aci-opflex":
			// These values are acceptable.
		default:
			return errors.New("Invalid fabric mode")
		}
		masterGc.NwInfraType = gc.NwInfraType
	}
	if gc.VLANs != "" {
		_, err := netutils.ParseTagRanges(gc.VLANs, "vlan")
		if err != nil {
			return err
		}
		gCfg.Auto.VLANs = gc.VLANs
		gcfgUpdateList = append(gcfgUpdateList, "vlan")
	}

	if gc.VXLANs != "" {
		_, err = netutils.ParseTagRanges(gc.VXLANs, "vxlan")
		if err != nil {
			return err
		}
		gCfg.Auto.VXLANs = gc.VXLANs
		gcfgUpdateList = append(gcfgUpdateList, "vxlan")
	}

	if gc.FwdMode != "" {
		masterGc.FwdMode = gc.FwdMode
	}

	if gc.ArpMode != "" {
		masterGc.ArpMode = gc.ArpMode
	}

	if len(gcfgUpdateList) > 0 {
		// Delete old state

		gOper := &gstate.Oper{}
		gOper.StateDriver = stateDriver
		err = gOper.Read("")
		if err == nil {
			for _, res := range gcfgUpdateList {
				err = gCfg.UpdateResources(res)
				if err != nil {
					return err
				}
			}
		} else {

			for _, res := range gcfgUpdateList {
				// setup resources
				err = gCfg.Process(res)
				if err != nil {
					log.Errorf("Error updating the config %+v. Error: %s", gCfg, err)
					return err
				}
			}
		}

		err = gCfg.Write()
		if err != nil {
			log.Errorf("error updating global config.Error: %s", err)
			return err
		}
	}
	return masterGc.Write()
}

// UpdateGlobal updates the global state
func UpdateGlobal(stateDriver core.StateDriver, gc *intent.ConfigGlobal) error {
	log.Infof("Received global update with intent {%v}", gc)
	var err error
	gcfgUpdateList := []string{}

	masterGc := &mastercfg.GlobConfig{}
	masterGc.StateDriver = stateDriver
	masterGc.Read("global")

	gstate.GlobalMutex.Lock()
	defer gstate.GlobalMutex.Unlock()

	gCfg := &gstate.Cfg{}
	gCfg.StateDriver = stateDriver
	gCfg.Read("global")

	_, vlansInUse := gCfg.GetVlansInUse()
	_, vxlansInUse := gCfg.GetVxlansInUse()

	// check for valid values
	if gc.NwInfraType != "" {
		switch gc.NwInfraType {
		case "default", "aci", "aci-opflex":
			// These values are acceptable.
		default:
			return errors.New("Invalid fabric mode")
		}
		masterGc.NwInfraType = gc.NwInfraType
	}
	if gc.VLANs != "" {

		if !gCfg.CheckInBitRange(gc.VLANs, vlansInUse, "vlan") {
			return fmt.Errorf("cannot update the vlan range due to existing vlans %s", vlansInUse)
		}
		_, err := netutils.ParseTagRanges(gc.VLANs, "vlan")
		if err != nil {
			return err
		}
		gCfg.Auto.VLANs = gc.VLANs
		gcfgUpdateList = append(gcfgUpdateList, "vlan")
	}

	if gc.VXLANs != "" {
		if !gCfg.CheckInBitRange(gc.VXLANs, vxlansInUse, "vxlan") {
			return fmt.Errorf("cannot update the vxlan range due to existing vxlans %s", vxlansInUse)
		}

		_, err = netutils.ParseTagRanges(gc.VXLANs, "vxlan")
		if err != nil {
			return err
		}
		gCfg.Auto.VXLANs = gc.VXLANs
		gcfgUpdateList = append(gcfgUpdateList, "vxlan")
	}

	if gc.FwdMode != "" {
		masterGc.FwdMode = gc.FwdMode
	}

	if gc.ArpMode != "" {
		masterGc.ArpMode = gc.ArpMode
	}

	if len(gcfgUpdateList) > 0 {
		// Delete old state

		gOper := &gstate.Oper{}
		gOper.StateDriver = stateDriver
		err = gOper.Read("")
		if err == nil {
			for _, res := range gcfgUpdateList {
				err = gCfg.UpdateResources(res)
				if err != nil {
					return err
				}
			}
		}

		err = gCfg.Write()
		if err != nil {
			log.Errorf("error updating global config.Error: %s", err)
			return err
		}
	}

	return masterGc.Write()
}

// DeleteGlobal delete global state
func DeleteGlobal(stateDriver core.StateDriver) error {
	masterGc := &mastercfg.GlobConfig{}
	masterGc.StateDriver = stateDriver
	err := masterGc.Read("")
	if err == nil {
		err = masterGc.Clear()
		if err != nil {
			return err
		}
	}

	// Setup global state
	gCfg := &gstate.Cfg{}
	gCfg.StateDriver = stateDriver
	err = gCfg.Read("")
	if err == nil {
		err = gCfg.DeleteResources("vlan")
		if err != nil {
			return err
		}
		err = gCfg.DeleteResources("vxlan")
		if err != nil {
			return err
		}

		err = gCfg.Clear()
		if err != nil {
			return err
		}
	}

	// Delete old state
	gOper := &gstate.Oper{}
	gOper.StateDriver = stateDriver
	err = gOper.Read("")
	if err == nil {
		err = gOper.Clear()
		if err != nil {
			return err
		}
	}

	return nil
}

// CreateTenant sets the tenant's state according to the passed ConfigTenant.
func CreateTenant(stateDriver core.StateDriver, tenant *intent.ConfigTenant) error {
	err := validateTenantConfig(tenant)
	if err != nil {
		return err
	}

	if IsDNSEnabled() {
		// start skydns container
		err = startServiceContainer(tenant.Name)
		if err != nil {
			log.Errorf("Error starting service container. Err: %v. Disabling DNS option.", err)
			SetDNSEnabled(false)
		}
	}

	return nil
}

func startServiceContainer(tenantName string) error {
	var err error
	docker, err := utils.GetDockerClient()
	if err != nil {
		log.Errorf("Unable to connect to docker. Error %v", err)
		return err
	}

	// pull the skydns image if it does not exist
	imageName := defaultSkyDNSImage
	_, err = docker.InspectImage(imageName)
	if err != nil {
		pullOperation := func() error {
			err := docker.PullImage(imageName, nil)
			if err != nil {
				log.Errorf("Retrying to pull image: %s", imageName)
				return err
			}
			return nil
		}

		err = backoff.Retry(pullOperation, backoff.NewExponentialBackOff())
		if err != nil {
			log.Errorf("Unable to pull image: %s", imageName)
			return err
		}
	}

	containerConfig := &dockerclient.ContainerConfig{
		Image: imageName,
		Env: []string{"ETCD_MACHINES=http://172.17.0.1:4001",
			"SKYDNS_NAMESERVERS=8.8.8.8:53",
			"SKYDNS_ADDR=0.0.0.0:53",
			"SKYDNS_DOMAIN=" + tenantName}}

	containerID, err := docker.CreateContainer(containerConfig, getDNSName(tenantName), nil)
	if err != nil {
		log.Errorf("Error creating DNS container for tenant: %s. Error: %s", tenantName, err)
		return err
	}

	hostConfig := &dockerclient.HostConfig{
		RestartPolicy: dockerclient.RestartPolicy{Name: "always"}}

	// Start the container
	err = docker.StartContainer(containerID, hostConfig)
	if err != nil {
		log.Errorf("Error starting DNS container for tenant: %s. Error: %s", tenantName, err)
	}

	return err
}

func stopAndRemoveServiceContainer(tenantName string) error {
	var err error
	docker, err := utils.GetDockerClient()
	if err != nil {
		log.Errorf("Unable to connect to docker. Error %v", err)
		return err
	}

	dnsContName := getDNSName(tenantName)
	// Stop the container
	err = docker.StopContainer(dnsContName, 10)
	if err != nil {
		log.Errorf("Error stopping DNS container for tenant: %s. Error: %s", tenantName, err)
		return err
	}

	err = docker.RemoveContainer(dnsContName, true, true)
	if err != nil {
		log.Errorf("Error removing DNS container for tenant: %s. Error: %s", tenantName, err)
		return err
	}
	return err
}

// DeleteTenantID deletes a tenant from the state store, by ID.
func DeleteTenantID(stateDriver core.StateDriver, tenantID string) error {
	if IsDNSEnabled() {
		err := stopAndRemoveServiceContainer(tenantID)
		if err != nil {
			log.Errorf("Error in stopping service container for tenant: %+v", tenantID)
			return err
		}
	}

	return nil
}

// DeleteTenant deletes a tenant from the state store based on its ConfigTenant.
func DeleteTenant(stateDriver core.StateDriver, tenant *intent.ConfigTenant) error {
	err := validateTenantConfig(tenant)
	if err != nil {
		return err
	}

	if len(tenant.Networks) == 0 {
		return DeleteTenantID(stateDriver, tenant.Name)
	}

	return nil
}

// IsAciConfigured returns true if aci is configured on netmaster.
func IsAciConfigured() (res bool, err error) {
	// Get the state driver
	stateDriver, uErr := utils.GetStateDriver()
	if uErr != nil {
		log.Warnf("Couldn't read global config %v", uErr)
		return false, uErr
	}

	// read global config
	masterGc := &mastercfg.GlobConfig{}
	masterGc.StateDriver = stateDriver
	uErr = masterGc.Read("config")
	if core.ErrIfKeyExists(uErr) != nil {
		log.Errorf("Couldn't read global config %v", uErr)
		return false, uErr
	}

	if uErr != nil {
		log.Warnf("Couldn't read global config %v", uErr)
		return false, nil
	}

	if masterGc.NwInfraType != "aci" {
		log.Debugf("NwInfra type is %v, no ACI", masterGc.NwInfraType)
		return false, nil
	}

	return true, nil
}
