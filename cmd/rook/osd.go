/*
Copyright 2016 The Rook Authors. All rights reserved.

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
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rook/rook/pkg/daemon/ceph/client"
	"github.com/rook/rook/pkg/daemon/ceph/mon"
	"github.com/rook/rook/pkg/daemon/ceph/osd"
	"github.com/rook/rook/pkg/operator/k8sutil"
	"github.com/rook/rook/pkg/util/flags"
	"github.com/spf13/cobra"
)

var osdCmd = &cobra.Command{
	Use:    "osd",
	Short:  "Generates osd config and runs the osd daemon",
	Hidden: true,
}
var (
	osdCluster          mon.ClusterInfo
	osdDataDeviceFilter string
)

func addOSDFlags(command *cobra.Command) {
	command.Flags().StringVar(&cfg.devices, "data-devices", "", "comma separated list of devices to use for storage")
	command.Flags().StringVar(&osdDataDeviceFilter, "data-device-filter", "", "a regex filter for the device names to use, or \"all\"")
	command.Flags().StringVar(&cfg.directories, "data-directories", "", "comma separated list of directory paths to use for storage")
	command.Flags().StringVar(&cfg.metadataDevice, "metadata-device", "", "device to use for metadata (e.g. a high performance SSD/NVMe device)")
	command.Flags().StringVar(&cfg.location, "location", "", "location of this node for CRUSH placement")
	command.Flags().BoolVar(&cfg.forceFormat, "force-format", false,
		"true to force the format of any specified devices, even if they already have a filesystem.  BE CAREFUL!")
	command.Flags().StringVar(&cfg.nodeName, "node-name", os.Getenv("HOSTNAME"), "the host name of the node")

	// OSD store config flags
	command.Flags().IntVar(&cfg.storeConfig.WalSizeMB, "osd-wal-size", osd.WalDefaultSizeMB, "default size (MB) for OSD write ahead log (WAL) (bluestore)")
	command.Flags().IntVar(&cfg.storeConfig.DatabaseSizeMB, "osd-database-size", osd.DBDefaultSizeMB, "default size (MB) for OSD database (bluestore)")
	command.Flags().IntVar(&cfg.storeConfig.JournalSizeMB, "osd-journal-size", osd.JournalDefaultSizeMB, "default size (MB) for OSD journal (filestore)")
	command.Flags().StringVar(&cfg.storeConfig.StoreType, "osd-store", osd.DefaultStore, "type of backing OSD store to use (bluestore or filestore)")
}

func init() {
	addOSDFlags(osdCmd)
	addCephFlags(osdCmd)
	flags.SetFlagsFromEnv(osdCmd.Flags(), RookEnvVarPrefix)

	osdCmd.RunE = startOSD
}

func startOSD(cmd *cobra.Command, args []string) error {
	required := []string{"cluster-name", "mon-endpoints", "mon-secret", "admin-secret", "node-name", "public-ipv4", "private-ipv4"}
	if err := flags.VerifyRequiredFlags(osdCmd, required); err != nil {
		return err
	}

	var dataDevices string
	var usingDeviceFilter bool
	if osdDataDeviceFilter != "" {
		if cfg.devices != "" {
			return fmt.Errorf("Only one of --data-devices and --data-device-filter can be specified.")
		}

		dataDevices = osdDataDeviceFilter
		usingDeviceFilter = true
	} else {
		dataDevices = cfg.devices
	}

	setLogLevel()

	logStartupInfo(osdCmd.Flags())

	clientset, _, rookClientset, err := getClientset()
	if err != nil {
		terminateFatal(fmt.Errorf("failed to init k8s client. %+v\n", err))
	}

	context := createContext()
	context.Clientset = clientset
	context.RookClientset = rookClientset

	kv := k8sutil.NewConfigMapKVStore(clusterInfo.Name, clientset)

	locArgs, err := client.FormatLocation(cfg.location, cfg.nodeName)
	if err != nil {
		terminateFatal(fmt.Errorf("invalid location. %+v\n", err))
	}
	crushLocation := strings.Join(locArgs, " ")

	forceFormat := false
	clusterInfo.Monitors = mon.ParseMonEndpoints(cfg.monEndpoints)
	agent := osd.NewAgent(context, dataDevices, usingDeviceFilter, cfg.metadataDevice, cfg.directories, forceFormat,
		crushLocation, cfg.storeConfig, &clusterInfo, cfg.nodeName, kv)

	err = osd.Run(context, agent)
	if err != nil {
		terminateFatal(err)
	}

	return nil
}
