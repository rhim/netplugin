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

package utils

import (
	log "github.com/Sirupsen/logrus"
)

func OvsDumpInfo(node TestbedNode) {
	cmdStr := "sudo ovs-vsctl show"
	output, _ := node.RunCommandWithOutput(cmdStr)
	log.Printf("ovs-vsctl on node %s: \n%s\n", node.GetName(), output)
}