/*
 * Tencent is pleased to support the open source community by making Blueking Container Service available.
 * Copyright (C) 2022 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 *
 * 	http://opensource.org/licenses/MIT
 *
 * Unless required by applicable law or agreed to in writing, software distributed under,
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package workload

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	resAction "github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/action/resource"
	"github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/action/util/perm"
	respUtil "github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/action/util/resp"
	"github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/common/errcode"
	res "github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/resource"
	cli "github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/resource/client"
	"github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/util/errorx"
	"github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/util/mapx"
	clusterRes "github.com/Tencent/bk-bcs/bcs-services/cluster-resources/proto/cluster-resources"
)

// ListPo 获取 Pod 列表
func (h *Handler) ListPo(
	_ context.Context, req *clusterRes.PodResListReq, resp *clusterRes.CommonResp,
) error {
	// 获取指定命名空间下的所有符合条件的 Pod
	ret, err := cli.NewPodCliByClusterID(req.ClusterID).List(
		req.Namespace, req.OwnerKind, req.OwnerName, metav1.ListOptions{LabelSelector: req.LabelSelector},
	)
	if err != nil {
		return err
	}
	resp.Data, err = respUtil.GenListResRespData(ret, res.Po)
	return err
}

// GetPo 获取单个 Pod
func (h *Handler) GetPo(
	_ context.Context, req *clusterRes.ResGetReq, resp *clusterRes.CommonResp,
) (err error) {
	resp.Data, err = resAction.NewResMgr(req.ProjectID, req.ClusterID, "", res.Po).Get(
		req.Namespace, req.Name, metav1.GetOptions{},
	)
	return err
}

// CreatePo 创建 Pod
func (h *Handler) CreatePo(
	_ context.Context, req *clusterRes.ResCreateReq, resp *clusterRes.CommonResp,
) (err error) {
	resp.Data, err = resAction.NewResMgr(req.ProjectID, req.ClusterID, "", res.Po).Create(
		req.Manifest, true, metav1.CreateOptions{},
	)
	return err
}

// UpdatePo 更新 Pod
func (h *Handler) UpdatePo(
	_ context.Context, req *clusterRes.ResUpdateReq, resp *clusterRes.CommonResp,
) (err error) {
	resp.Data, err = resAction.NewResMgr(req.ProjectID, req.ClusterID, "", res.Po).Update(
		req.Namespace, req.Name, req.Manifest, metav1.UpdateOptions{},
	)
	return err
}

// DeletePo 删除 Pod
func (h *Handler) DeletePo(
	_ context.Context, req *clusterRes.ResDeleteReq, _ *clusterRes.CommonResp,
) error {
	return resAction.NewResMgr(req.ProjectID, req.ClusterID, "", res.Po).Delete(
		req.Namespace, req.Name, metav1.DeleteOptions{},
	)
}

// ListPoPVC 获取 Pod PVC 列表
func (h *Handler) ListPoPVC(
	_ context.Context, req *clusterRes.ResGetReq, resp *clusterRes.CommonResp,
) (err error) {
	if err := perm.CheckNSAccess(req.ProjectID, req.ClusterID, req.Namespace); err != nil {
		return err
	}
	resp.Data, err = respUtil.BuildListPodRelatedResResp(req.ClusterID, req.Namespace, req.Name, res.PVC)
	return err
}

// ListPoCM 获取 Pod ConfigMap 列表
func (h *Handler) ListPoCM(
	_ context.Context, req *clusterRes.ResGetReq, resp *clusterRes.CommonResp,
) (err error) {
	if err := perm.CheckNSAccess(req.ProjectID, req.ClusterID, req.Namespace); err != nil {
		return err
	}
	resp.Data, err = respUtil.BuildListPodRelatedResResp(req.ClusterID, req.Namespace, req.Name, res.CM)
	return err
}

// ListPoSecret 获取 Pod Secret 列表
func (h *Handler) ListPoSecret(
	_ context.Context, req *clusterRes.ResGetReq, resp *clusterRes.CommonResp,
) (err error) {
	if err := perm.CheckNSAccess(req.ProjectID, req.ClusterID, req.Namespace); err != nil {
		return err
	}
	resp.Data, err = respUtil.BuildListPodRelatedResResp(req.ClusterID, req.Namespace, req.Name, res.Secret)
	return err
}

// ReschedulePo 重新调度 Pod
func (h *Handler) ReschedulePo(
	_ context.Context, req *clusterRes.ResUpdateReq, _ *clusterRes.CommonResp,
) (err error) {
	if err := perm.CheckNSAccess(req.ProjectID, req.ClusterID, req.Namespace); err != nil {
		return err
	}

	podManifest, err := cli.NewPodCliByClusterID(req.ClusterID).GetManifest(req.Namespace, req.Name)
	if err != nil {
		return err
	}

	// 检查 Pod 配置，必须有父级资源且不为 Job 才可以重新调度
	ownerReferences, err := mapx.GetItems(podManifest, "metadata.ownerReferences")
	if err != nil {
		return errorx.New(errcode.Unsupported, "Pod %s/%s 不存在父级资源，不允许重新调度", req.Namespace, req.Name)
	}
	// 检查确保父级资源不为 Job
	for _, ref := range ownerReferences.([]interface{}) {
		if ref.(map[string]interface{})["kind"].(string) == res.Job {
			return errorx.New(errcode.Unsupported, "Pod %s/%s 父级资源存在 Job，不允许重新调度", req.Namespace, req.Name)
		}
	}

	// 重新调度的原理是直接删除 Pod，利用父级资源重新拉起服务
	return respUtil.BuildDeleteAPIResp(
		req.ClusterID, res.Po, "", req.Namespace, req.Name, metav1.DeleteOptions{},
	)
}
