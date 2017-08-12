/**
 * Copyright (C) 2015 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *         http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package cmds

import (
	"github.com/fabric8io/gofabric8/client"
	"github.com/fabric8io/gofabric8/util"
	"github.com/spf13/cobra"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"

	selection "github.com/openshift/origin/pkg/util/labelselector"
	apim "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
)

func NewCmdPackages(f cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "packages",
		Short: "Lists the packages that are currently installed",
		Long:  `Lists the packages that are currently installed`,
		Run: func(cmd *cobra.Command, args []string) {
			c, cfg := client.NewClient(f)
			ns, _, err := f.DefaultNamespace()
			if err != nil {
				util.Fatal("No default namespace")
				printResult("Get default namespace", Failure, err)
			} else {
				util.Info("Packages in your ")
				util.Success(string(util.TypeOfMaster(c)))
				util.Info(" installation at ")
				util.Success(cfg.Host)
				util.Info(" in namespace ")
				util.Successf("%s\n\n", ns)
				err := listPackages(ns, c, f)
				if err != nil {
					util.Failuref("%v", err)
					util.Blank()
				}
			}
		},
	}
	return cmd
}

func createPackageSelector() (*labels.Selector, error) {
	req, err := labels.NewRequirement(
		"fabric8.io/kind", selection.Equals, []string{"package"})
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*req)
	return &selector, nil
}

func listPackages(ns string, c *clientset.Clientset, fac cmdutil.Factory) error {
	selector, err := createPackageSelector()
	if err != nil {
		return err
	}
	list, err := c.ConfigMaps(ns).List(apim.ListOptions{
		LabelSelector: *selector,
	})
	if err != nil {
		util.Errorf("Failed to load package in namespace %s with error %v", ns, err)
		return err
	}
	for _, p := range list.Items {
		version := ""
		labels := p.Labels
		if labels != nil {
			version = labels["version"]
		}
		util.Success(p.Name)
		util.Info(" version: ")
		util.Success(version)
		util.Info("\n")
	}
	return nil
}
