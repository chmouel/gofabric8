/*
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
	"errors"
	"fmt"
	"io"
	"strconv"

	"strings"

	"github.com/fabric8io/gofabric8/client"
	"github.com/fabric8io/gofabric8/util"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
	apim "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/api"
	k8api "k8s.io/kubernetes/pkg/api/unversioned"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
)

type EnvironmentData struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Order     int    `yaml:"order"`
}

func NewCmdGetEnviron(f cmdutil.Factory, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "environ",
		Short:   "Get environment from fabric8-environments configmap",
		Aliases: []string{"env"},
		Run: func(cmd *cobra.Command, args []string) {
			wp := cmd.Flags().Lookup("work-project").Value.String()
			detectedNS, c, _ := getOpenShiftClient(f, wp)
			err := getEnviron(cmd, args, detectedNS, c)
			cmdutil.CheckErr(err)
		},
	}
	return cmd
}

func getEnviron(cmd *cobra.Command, args []string, detectedNS string, c *clientset.Clientset) (err error) {
	selector, err := k8api.LabelSelectorAsSelector(
		&k8api.LabelSelector{MatchLabels: map[string]string{"kind": "environments"}})
	cmdutil.CheckErr(err)

	cfgmap, err := c.ConfigMaps(detectedNS).List(apim.ListOptions{LabelSelector: selector})
	cmdutil.CheckErr(err)

	fmt.Printf("%-10s DATA\n", "ENV")
	for _, item := range cfgmap.Items {
		for key, data := range item.Data {
			var ed EnvironmentData
			err := yaml.Unmarshal([]byte(data), &ed)
			if err != nil {
				return err
			}
			fmt.Printf("%-10s namespace=%s order=%d\n",
				key, ed.Namespace, ed.Order)
		}
	}
	return
}

func NewCmdCreateEnviron(f cmdutil.Factory) (cmd *cobra.Command) {
	cmd = &cobra.Command{
		Use:     "environ",
		Short:   "Create environment from fabric8-environments configmap",
		Long:    "gofabric8 create environ environKey namespace=string order=int ...",
		Aliases: []string{"env"},
		Run: func(cmd *cobra.Command, args []string) {
			wp := cmd.Flags().Lookup("work-project").Value.String()
			detectedNS, c, _ := getOpenShiftClient(f, wp)
			err := createEnviron(cmd, args, detectedNS, c)
			cmdutil.CheckErr(err)
			cmd.Help()
		},
	}
	return
}

func createEnviron(cmd *cobra.Command, args []string, detectedNS string, c *clientset.Clientset) (err error) {
	var ev EnvironmentData
	var yamlData []byte
	ev.Order = -1

	for _, kv := range args {
		split := strings.Split(kv, "=")
		if len(split) == 1 {
			return errors.New("Invalid argument " + kv + " you are missing an assignment like foo=bar")
		}
		k := split[0]
		v := split[1]

		if strings.ToLower(k) == "name" {
			ev.Name = strings.ToLower(v)
		} else if strings.ToLower(k) == "namespace" {
			ev.Namespace = v
		} else if strings.ToLower(k) == "order" {
			conv, err := strconv.Atoi(v)
			if err != nil {
				return errors.New(fmt.Sprintf("Cannot use %s from %s as number: %v\n", v, k, err))
			}
			ev.Order = conv
		} else {
			util.Errorf("Unkown key: %s\n", k)
			return
		}
	}

	if ev.Name == "" || ev.Namespace == "" || ev.Order == -1 {
		return errors.New("missing some key=value\n")
	}

	yamlData, err = yaml.Marshal(&ev)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to marshal configmap fabric8-environments error: %v\ntemplate: %s", err, string(yamlData)))
	}

	selector, err := k8api.LabelSelectorAsSelector(
		&k8api.LabelSelector{MatchLabels: map[string]string{"kind": "environments"}})
	if err != nil {
		return err
	}

	cfgmaps, err := c.ConfigMaps(detectedNS).List(apim.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}

	cfgmap := &cfgmaps.Items[0] // TODO(chmou): can we have more than one cfgmap with kind=environments label?
	cfgmap.Data[ev.Name] = string(yamlData)
	cfgmap.Name = ev.Name

	_, err = c.ConfigMaps(detectedNS).Update(cfgmap)

	return
}

// NewCmdDeleteEnviron is a command to delete an environ using: gofabric8 delete environ abcd
func NewCmdDeleteEnviron(f cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "env",
		Short:   "Delete environment from fabric8-environments configmap",
		Aliases: []string{"environ", "enviroment"},
		Run: func(cmd *cobra.Command, args []string) {
			wp := cmd.Flags().Lookup("work-project").Value.String()
			detectedNS, c, _ := getOpenShiftClient(f, wp)

			selector, err := k8api.LabelSelectorAsSelector(
				&k8api.LabelSelector{MatchLabels: map[string]string{"kind": "environments"}})
			cmdutil.CheckErr(err)

			if len(args) == 0 {
				util.Errorf("Delete command requires the name of the environment as a parameter\n")
				return
			}

			if len(args) != 1 {
				util.Errorf("Delete command can have only one environment name parameter\n")
				return
			}

			toDeleteEnv := args[0]

			cfgmap, err := c.ConfigMaps(detectedNS).List(apim.ListOptions{LabelSelector: selector})
			cmdutil.CheckErr(err)

			// remove the entry from the config map
			var updatedCfgMap *api.ConfigMap
			for _, item := range cfgmap.Items {
				for k, data := range item.Data {
					var ed EnvironmentData

					err := yaml.Unmarshal([]byte(data), &ed)
					cmdutil.CheckErr(err)

					if strings.ToLower(ed.Name) == strings.ToLower(toDeleteEnv) {
						delete(item.Data, k)
						updatedCfgMap = &item
						goto DeletedConfig
					}

				}
			}

		DeletedConfig:
			if updatedCfgMap == nil {
				util.Warnf("Could not find environment named %s\n", toDeleteEnv)
				return
			}

			_, err = c.ConfigMaps(detectedNS).Update(updatedCfgMap)
			if err != nil {
				util.Errorf("Failed to update config map after deleting: %v\n", err)
				return
			}
			util.Infof("environment %s has been deleted\n", toDeleteEnv)
		},
	}
	return cmd
}

// getOpenShiftClient Get an openshift client and detect the project we want to
// be in
func getOpenShiftClient(f cmdutil.Factory, wp string) (detectedNS string, c *clientset.Clientset, cfg *restclient.Config) {
	c, cfg = client.NewClient(f)

	// If the user has specified a userproject then don't do auto detection and
	// use it immediatly.
	if wp != "autodetect" {
		detectedNS = wp
		return
	}

	initSchema()

	typeOfMaster := util.TypeOfMaster(c)
	isOpenshift := typeOfMaster == util.OpenShift

	if isOpenshift {
		oc, _ := client.NewOpenShiftClient(cfg)
		projects, err := oc.Projects().List(apim.ListOptions{})
		if err != nil {
			util.Warnf("Could not list projects: %v", err)
		} else {
			currentNS, _, _ := f.DefaultNamespace()
			detectedNS = detectCurrentUserProject(currentNS, projects.Items, c)
		}
	}

	if detectedNS == "" {
		detectedNS, _, _ = f.DefaultNamespace()
	}

	return
}
