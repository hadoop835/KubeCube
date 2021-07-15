/*
Copyright 2021 KubeCube Authors

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

package cluster

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/kubecube-io/kubecube/pkg/utils/kubeconfig"

	"github.com/kubecube-io/kubecube/pkg/utils/env"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/kubecube-io/kubecube/pkg/multicluster"
	"github.com/kubecube-io/kubecube/pkg/utils/strproc"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multi-tenancy/incubator/hnc/api/v1alpha2"

	"github.com/kubecube-io/kubecube/pkg/quota"
	v1 "k8s.io/api/core/v1"

	"github.com/kubecube-io/kubecube/pkg/clients/kubernetes"

	"github.com/gin-gonic/gin"
	clusterv1 "github.com/kubecube-io/kubecube/pkg/apis/cluster/v1"
	"github.com/kubecube-io/kubecube/pkg/clients"
	"github.com/kubecube-io/kubecube/pkg/clog"
	"github.com/kubecube-io/kubecube/pkg/utils/constants"
	"github.com/kubecube-io/kubecube/pkg/utils/errcode"
	"github.com/kubecube-io/kubecube/pkg/utils/response"
	"k8s.io/apimachinery/pkg/types"
)

const subPath = "clusters"

func AddApisTo(root *gin.RouterGroup) {
	h := newHandler()
	r := root.Group(subPath)
	r.GET("info", h.getClusterInfo)
	r.GET("namespaces", h.getClusterNames)
	r.GET("resources", h.getClusterResource)
	r.GET("subnamespaces", h.getSubNamespaces)
	r.POST("register", h.registerCluster)
	r.POST("add", h.addCluster)
}

type result struct {
	Total int           `json:"total"`
	Items []clusterInfo `json:"items"`
}

type clusterInfo struct {
	ClusterName           string    `json:"clusterName"`
	ClusterDescription    string    `json:"clusterDescription"`
	NetworkType           string    `json:"networkType"`
	HarborAddr            string    `json:"harborAddr"`
	NodeCount             int       `json:"nodeCount"`
	TotalCPU              int       `json:"totalCpu"`
	UsedCPU               int       `json:"usedCpu"`
	NamespaceCount        int       `json:"namespaceCount"`
	TotalMem              int       `json:"totalMem"`
	UsedMem               int       `json:"usedMem"`
	TotalStorage          int       `json:"totalStorage"`
	UsedStorage           int       `json:"usedStorage"`
	TotalStorageEphemeral int       `json:"totalStorageEphemeral"`
	UsedStorageEphemeral  int       `json:"usedStorageEphemeral"`
	TotalGpu              int       `json:"totalGpu"`
	UsedGpu               int       `json:"usedGpu"`
	IsMemberCluster       bool      `json:"isMemberCluster"`
	CreateTime            time.Time `json:"createTime"`
	KubeApiServer         string    `json:"kubeApiServer"`
	Status                string    `json:"status"`
}

type handler struct {
	kubernetes.Client
}

func newHandler() *handler {
	h := new(handler)
	h.Client = clients.Interface().Kubernetes(constants.PivotCluster)
	return h
}

// getClusterInfo get cluster details by cluster name
// @Summary Show cluster info
// @Description get cluster info by cluster name, non cluster name means all clusters info
// @Tags cluster
// @Param cluster query string false "cluster info search by cluster"
// @Success 200 {object} result
// @Failure 500 {object} errcode.ErrorInfo
// @Router /api/v1/cube/clusters/info  [get]
func (h *handler) getClusterInfo(c *gin.Context) {
	var (
		cli         = h.Client
		ctx         = c.Request.Context()
		clusterList = clusterv1.ClusterList{}
	)

	clusterName := c.Query("cluster")

	if len(clusterName) > 0 {
		key := types.NamespacedName{Name: clusterName}
		cluster := clusterv1.Cluster{}
		err := cli.Cache().Get(ctx, key, &cluster)
		if err != nil {
			clog.Error("get cluster failed: %v", err)
			response.FailReturn(c, errcode.InternalServerError)
			return
		}
		clusterList = clusterv1.ClusterList{Items: []clusterv1.Cluster{cluster}}
	} else {
		clusters := clusterv1.ClusterList{}
		err := cli.Cache().List(ctx, &clusters)
		if err != nil {
			clog.Error("list cluster failed: %v", err)
			response.FailReturn(c, errcode.InternalServerError)
			return
		}
		clusterList = clusters
	}

	infos := makeClusterInfos(clusterList, cli, c)
	if infos != nil {
		res := result{
			Total: len(clusterList.Items),
			Items: infos,
		}
		response.SuccessReturn(c, res)
	}

	return
}

// getClusterNames get cluster name where the namespace work in
// @Summary Show all clusters bind to namespace
// @Description get cluster name where the namespace work in
// @Tags cluster
// @Param namespace query string false "clusters search by namespace"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} errcode.ErrorInfo
// @Router /api/v1/cube/clusters/namespaces  [get]
func (h *handler) getClusterNames(c *gin.Context) {
	var (
		namespace    = c.Query("namespace")
		ctx          = c.Request.Context()
		clusterNames []string
	)

	if len(namespace) > 0 {
		clusters, err := getClustersByNamespace(namespace, ctx)
		if err != nil {
			response.FailReturn(c, errcode.InternalServerError)
			return
		}
		clusterNames = clusters
	} else {
		clusterNames = listClusterNames()
	}

	res := map[string]interface{}{
		"total": len(clusterNames),
		"items": clusterNames,
	}

	response.SuccessReturn(c, res)
}

// getClusterResource get allocate resource of cluster
// @Summary Get allocate resource of cluster
// @Description get allocate resource of cluster
// @Tags cluster
// @Param cluster query string true "allocate resource search by cluster"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} errcode.ErrorInfo
// @Router /api/v1/cube/clusters/resources  [get]
func (h *handler) getClusterResource(c *gin.Context) {
	cluster := c.Query("cluster")
	cli := clients.Interface().Kubernetes(cluster)
	if cli == nil {
		response.FailReturn(c, errcode.ClusterNotFoundError(cluster))
		return
	}

	nodes := v1.NodeList{}
	err := cli.Cache().List(c.Request.Context(), &nodes)
	if err != nil {
		clog.Error("get cluster %v nodes failed: %v", cluster, err)
		response.FailReturn(c, errcode.InternalServerError)
	}

	capacityCpu := quota.ZeroQ()
	capacityMem := quota.ZeroQ()
	capacityGpu := quota.ZeroQ()

	for _, v := range nodes.Items {
		capacityCpu.Add(*v.Status.Capacity.Cpu())
		capacityMem.Add(*v.Status.Capacity.Memory())
		nodeGpu, ok := v.Status.Capacity[quota.ResourceNvidiaGPU]
		if ok {
			capacityCpu.Add(nodeGpu)
		}
	}

	assignedCpu, assignedMem, assignedGpu, err := getAssignedResource(cli, cluster)
	if err != nil {
		clog.Error(err.Error())
		response.FailReturn(c, errcode.InternalServerError)
		return
	}

	res := map[string]interface{}{
		"capacityCpu": capacityCpu,
		"assignedCpu": assignedCpu,
		"assignedMem": assignedMem,
		"capacityGpu": capacityGpu,
		"assignedGpu": assignedGpu,
		"capacityMem": fmt.Sprintf("%vMi", strproc.Str2int(capacityMem.String())/1024),
	}

	response.SuccessReturn(c, res)
}

// getSubNamespaces list sub namespace by tenant
// @Summary Get sub namespace
// @Description get sub namespaces by tenant
// @Tags cluster
// @Param tenant query string false "list sub namespaces by tenant"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} errcode.ErrorInfo
// @Router /api/v1/cube/clusters/subnamespaces  [get]
func (h *handler) getSubNamespaces(c *gin.Context) {
	tenant := c.Query("tenant")
	ctx := c.Request.Context()
	clusters := multicluster.Interface().FuzzyCopy()

	listFunc := func(cli kubernetes.Client) (v1alpha2.SubnamespaceAnchorList, error) {
		anchors := v1alpha2.SubnamespaceAnchorList{}
		err := cli.Cache().List(ctx, &anchors)
		return anchors, err
	}

	if len(tenant) > 0 {
		labelSelector, err := labels.Parse(fmt.Sprintf("%v=%v", constants.TenantLabel, tenant))
		if err != nil {
			clog.Error(err.Error())
			response.FailReturn(c, errcode.CustomReturn(http.StatusInternalServerError, "label selector parse failed"))
			return
		}
		listFunc = func(cli kubernetes.Client) (v1alpha2.SubnamespaceAnchorList, error) {
			anchors := v1alpha2.SubnamespaceAnchorList{}
			err := cli.Cache().List(ctx, &anchors, &client.ListOptions{LabelSelector: labelSelector})
			return anchors, err
		}
	}

	items := make([]map[string]string, 0)

	// search in every cluster
	for _, cluster := range clusters {
		cli := cluster.Client
		anchors, err := listFunc(cli)
		if err != nil {
			clog.Error(err.Error())
			continue
		}

		for _, anchor := range anchors.Items {
			project, ok := anchor.Labels[constants.ProjectLabel]
			if ok && anchor.ObjectMeta.DeletionTimestamp.IsZero() {
				item := map[string]string{
					"namespace": anchor.Name,
					"cluster":   cluster.Name,
					"project":   project,
				}
				items = append(items, item)
			}
		}
	}

	res := map[string]interface{}{
		"total": len(items),
		"items": items,
	}

	response.SuccessReturn(c, res)
}

// scriptData is the data to render script
type scriptData struct {
	ClusterName  string `json:"clusterName"`
	KubeConfig   string `json:"kubeConfig"`
	K8sEndpoint  string `json:"k8sEndpoint,omitempty"`
	NetworkType  string `json:"networkType,omitempty"`
	Description  string `json:"description,omitempty"`
	KubeCubeHost string `json:"kubeCubeHost,omitempty"`
}

// addCluster return script which need be execute in member cluster node
// @Summary Add cluster
// @Description add cluster to KubeCube
// @Tags cluster
// @Param scriptData body scriptData true "new cluster raw data"
// @Success 200 {object} string "base64 encode"
// @Failure 400 {object} errcode.ErrorInfo
// @Failure 500 {object} errcode.ErrorInfo
// @Router /api/v1/cube/clusters/addCluster  [post]
func (h *handler) addCluster(c *gin.Context) {
	const (
		defaultNetworkType string = "calico"
		defaultDescription        = "this is member cluster"
	)

	d := scriptData{}
	err := c.ShouldBindJSON(&d)
	if err != nil {
		clog.Error(err.Error())
		response.FailReturn(c, errcode.CustomReturn(http.StatusBadRequest, err.Error()))
		return
	}

	if len(d.Description) == 0 {
		d.Description = defaultDescription
	}

	if len(d.NetworkType) == 0 {
		d.NetworkType = defaultNetworkType
	}

	if len(d.KubeCubeHost) == 0 {
		d.KubeCubeHost = env.NodeIP()
	}

	kubeConfig, err := base64.StdEncoding.DecodeString(d.KubeConfig)
	if err != nil {
		clog.Warn(err.Error())
		response.FailReturn(c, errcode.CustomReturn(http.StatusBadRequest, "kubeConfig invalid: %v", err))
		return
	}

	config, err := kubeconfig.LoadKubeConfigFromBytes(kubeConfig)
	if err != nil {
		clog.Warn(err.Error())
		response.FailReturn(c, errcode.CustomReturn(http.StatusBadRequest, "kubeConfig invalid: %v", err))
		return
	}

	if len(d.K8sEndpoint) == 0 {
		d.K8sEndpoint = config.Host
	}

	var s string
	w := bytes.NewBufferString(s)

	err = scriptTemplate.Execute(w, d)
	if err != nil {
		clog.Error(err.Error())
		response.FailReturn(c, errcode.CustomReturn(http.StatusBadRequest, err.Error()))
		return
	}

	res := base64.StdEncoding.EncodeToString(w.Bytes())

	response.SuccessReturn(c, res)
}

// registerCluster is a callback api for add cluster to pivot cluster
func (h *handler) registerCluster(c *gin.Context) {
	cluster := &clusterv1.Cluster{}
	err := c.ShouldBindJSON(cluster)
	if err != nil {
		clog.Error(err.Error())
		response.FailReturn(c, errcode.CustomReturn(http.StatusBadRequest, err.Error()))
		return
	}

	err = h.Direct().Create(c.Request.Context(), cluster)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			clog.Warn(err.Error())
		} else {
			clog.Error(err.Error())
			response.FailReturn(c, errcode.CustomReturn(http.StatusInternalServerError, err.Error()))
			return
		}
	}

	response.SuccessReturn(c, "success")
}