// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.34.1
// 	protoc        (unknown)
// source: api/v1/inference_server_management.proto

package v1

import (
	_ "google.golang.org/genproto/googleapis/api/annotations"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type EngineStatus struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	EngineId string `protobuf:"bytes,1,opt,name=engine_id,json=engineId,proto3" json:"engine_id,omitempty"`
	// Deprecated: Marked as deprecated in api/v1/inference_server_management.proto.
	ModelIds []string `protobuf:"bytes,2,rep,name=model_ids,json=modelIds,proto3" json:"model_ids,omitempty"`
	// Deprecated: Marked as deprecated in api/v1/inference_server_management.proto.
	SyncStatus *EngineStatus_SyncStatus `protobuf:"bytes,3,opt,name=sync_status,json=syncStatus,proto3" json:"sync_status,omitempty"`
	Ready      bool                     `protobuf:"varint,4,opt,name=ready,proto3" json:"ready,omitempty"`
	Models     []*EngineStatus_Model    `protobuf:"bytes,5,rep,name=models,proto3" json:"models,omitempty"`
	ClusterId  string                   `protobuf:"bytes,6,opt,name=cluster_id,json=clusterId,proto3" json:"cluster_id,omitempty"`
}

func (x *EngineStatus) Reset() {
	*x = EngineStatus{}
	if protoimpl.UnsafeEnabled {
		mi := &file_api_v1_inference_server_management_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *EngineStatus) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EngineStatus) ProtoMessage() {}

func (x *EngineStatus) ProtoReflect() protoreflect.Message {
	mi := &file_api_v1_inference_server_management_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EngineStatus.ProtoReflect.Descriptor instead.
func (*EngineStatus) Descriptor() ([]byte, []int) {
	return file_api_v1_inference_server_management_proto_rawDescGZIP(), []int{0}
}

func (x *EngineStatus) GetEngineId() string {
	if x != nil {
		return x.EngineId
	}
	return ""
}

// Deprecated: Marked as deprecated in api/v1/inference_server_management.proto.
func (x *EngineStatus) GetModelIds() []string {
	if x != nil {
		return x.ModelIds
	}
	return nil
}

// Deprecated: Marked as deprecated in api/v1/inference_server_management.proto.
func (x *EngineStatus) GetSyncStatus() *EngineStatus_SyncStatus {
	if x != nil {
		return x.SyncStatus
	}
	return nil
}

func (x *EngineStatus) GetReady() bool {
	if x != nil {
		return x.Ready
	}
	return false
}

func (x *EngineStatus) GetModels() []*EngineStatus_Model {
	if x != nil {
		return x.Models
	}
	return nil
}

func (x *EngineStatus) GetClusterId() string {
	if x != nil {
		return x.ClusterId
	}
	return ""
}

type InferenceStatus struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	ClusterStatuses []*ClusterStatus `protobuf:"bytes,1,rep,name=cluster_statuses,json=clusterStatuses,proto3" json:"cluster_statuses,omitempty"`
	TaskStatus      *TaskStatus      `protobuf:"bytes,2,opt,name=task_status,json=taskStatus,proto3" json:"task_status,omitempty"`
}

func (x *InferenceStatus) Reset() {
	*x = InferenceStatus{}
	if protoimpl.UnsafeEnabled {
		mi := &file_api_v1_inference_server_management_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *InferenceStatus) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*InferenceStatus) ProtoMessage() {}

func (x *InferenceStatus) ProtoReflect() protoreflect.Message {
	mi := &file_api_v1_inference_server_management_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use InferenceStatus.ProtoReflect.Descriptor instead.
func (*InferenceStatus) Descriptor() ([]byte, []int) {
	return file_api_v1_inference_server_management_proto_rawDescGZIP(), []int{1}
}

func (x *InferenceStatus) GetClusterStatuses() []*ClusterStatus {
	if x != nil {
		return x.ClusterStatuses
	}
	return nil
}

func (x *InferenceStatus) GetTaskStatus() *TaskStatus {
	if x != nil {
		return x.TaskStatus
	}
	return nil
}

type TaskStatus struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// in_progress_task_counts tracks the number of in-progress tasks grouped by model id.
	InProgressTaskCounts map[string]int32 `protobuf:"bytes,1,rep,name=in_progress_task_counts,json=inProgressTaskCounts,proto3" json:"in_progress_task_counts,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"varint,2,opt,name=value,proto3"`
}

func (x *TaskStatus) Reset() {
	*x = TaskStatus{}
	if protoimpl.UnsafeEnabled {
		mi := &file_api_v1_inference_server_management_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *TaskStatus) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TaskStatus) ProtoMessage() {}

func (x *TaskStatus) ProtoReflect() protoreflect.Message {
	mi := &file_api_v1_inference_server_management_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TaskStatus.ProtoReflect.Descriptor instead.
func (*TaskStatus) Descriptor() ([]byte, []int) {
	return file_api_v1_inference_server_management_proto_rawDescGZIP(), []int{2}
}

func (x *TaskStatus) GetInProgressTaskCounts() map[string]int32 {
	if x != nil {
		return x.InProgressTaskCounts
	}
	return nil
}

type ClusterStatus struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Id   string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Name string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	// TODO(kenji): Revisit. Each engine in the same cluster reports the same information on models.
	// It might be better to just report the model information.
	EngineStatuses      []*EngineStatus `protobuf:"bytes,3,rep,name=engine_statuses,json=engineStatuses,proto3" json:"engine_statuses,omitempty"`
	ModelCount          int32           `protobuf:"varint,4,opt,name=model_count,json=modelCount,proto3" json:"model_count,omitempty"`
	InProgressTaskCount int32           `protobuf:"varint,5,opt,name=in_progress_task_count,json=inProgressTaskCount,proto3" json:"in_progress_task_count,omitempty"`
	GpuAllocated        int32           `protobuf:"varint,6,opt,name=gpu_allocated,json=gpuAllocated,proto3" json:"gpu_allocated,omitempty"`
}

func (x *ClusterStatus) Reset() {
	*x = ClusterStatus{}
	if protoimpl.UnsafeEnabled {
		mi := &file_api_v1_inference_server_management_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ClusterStatus) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ClusterStatus) ProtoMessage() {}

func (x *ClusterStatus) ProtoReflect() protoreflect.Message {
	mi := &file_api_v1_inference_server_management_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ClusterStatus.ProtoReflect.Descriptor instead.
func (*ClusterStatus) Descriptor() ([]byte, []int) {
	return file_api_v1_inference_server_management_proto_rawDescGZIP(), []int{3}
}

func (x *ClusterStatus) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *ClusterStatus) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *ClusterStatus) GetEngineStatuses() []*EngineStatus {
	if x != nil {
		return x.EngineStatuses
	}
	return nil
}

func (x *ClusterStatus) GetModelCount() int32 {
	if x != nil {
		return x.ModelCount
	}
	return 0
}

func (x *ClusterStatus) GetInProgressTaskCount() int32 {
	if x != nil {
		return x.InProgressTaskCount
	}
	return 0
}

func (x *ClusterStatus) GetGpuAllocated() int32 {
	if x != nil {
		return x.GpuAllocated
	}
	return 0
}

type GetInferenceStatusRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields
}

func (x *GetInferenceStatusRequest) Reset() {
	*x = GetInferenceStatusRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_api_v1_inference_server_management_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *GetInferenceStatusRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetInferenceStatusRequest) ProtoMessage() {}

func (x *GetInferenceStatusRequest) ProtoReflect() protoreflect.Message {
	mi := &file_api_v1_inference_server_management_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetInferenceStatusRequest.ProtoReflect.Descriptor instead.
func (*GetInferenceStatusRequest) Descriptor() ([]byte, []int) {
	return file_api_v1_inference_server_management_proto_rawDescGZIP(), []int{4}
}

type EngineStatus_SyncStatus struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// in_progress_model_ids is a list of model ids that are currently being synced.
	InProgressModelIds []string `protobuf:"bytes,1,rep,name=in_progress_model_ids,json=inProgressModelIds,proto3" json:"in_progress_model_ids,omitempty"`
}

func (x *EngineStatus_SyncStatus) Reset() {
	*x = EngineStatus_SyncStatus{}
	if protoimpl.UnsafeEnabled {
		mi := &file_api_v1_inference_server_management_proto_msgTypes[5]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *EngineStatus_SyncStatus) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EngineStatus_SyncStatus) ProtoMessage() {}

func (x *EngineStatus_SyncStatus) ProtoReflect() protoreflect.Message {
	mi := &file_api_v1_inference_server_management_proto_msgTypes[5]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EngineStatus_SyncStatus.ProtoReflect.Descriptor instead.
func (*EngineStatus_SyncStatus) Descriptor() ([]byte, []int) {
	return file_api_v1_inference_server_management_proto_rawDescGZIP(), []int{0, 0}
}

func (x *EngineStatus_SyncStatus) GetInProgressModelIds() []string {
	if x != nil {
		return x.InProgressModelIds
	}
	return nil
}

type EngineStatus_Model struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Id                  string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	IsReady             bool   `protobuf:"varint,2,opt,name=is_ready,json=isReady,proto3" json:"is_ready,omitempty"`
	InProgressTaskCount int32  `protobuf:"varint,3,opt,name=in_progress_task_count,json=inProgressTaskCount,proto3" json:"in_progress_task_count,omitempty"`
	GpuAllocated        int32  `protobuf:"varint,4,opt,name=gpu_allocated,json=gpuAllocated,proto3" json:"gpu_allocated,omitempty"`
}

func (x *EngineStatus_Model) Reset() {
	*x = EngineStatus_Model{}
	if protoimpl.UnsafeEnabled {
		mi := &file_api_v1_inference_server_management_proto_msgTypes[6]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *EngineStatus_Model) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EngineStatus_Model) ProtoMessage() {}

func (x *EngineStatus_Model) ProtoReflect() protoreflect.Message {
	mi := &file_api_v1_inference_server_management_proto_msgTypes[6]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use EngineStatus_Model.ProtoReflect.Descriptor instead.
func (*EngineStatus_Model) Descriptor() ([]byte, []int) {
	return file_api_v1_inference_server_management_proto_rawDescGZIP(), []int{0, 1}
}

func (x *EngineStatus_Model) GetId() string {
	if x != nil {
		return x.Id
	}
	return ""
}

func (x *EngineStatus_Model) GetIsReady() bool {
	if x != nil {
		return x.IsReady
	}
	return false
}

func (x *EngineStatus_Model) GetInProgressTaskCount() int32 {
	if x != nil {
		return x.InProgressTaskCount
	}
	return 0
}

func (x *EngineStatus_Model) GetGpuAllocated() int32 {
	if x != nil {
		return x.GpuAllocated
	}
	return 0
}

var File_api_v1_inference_server_management_proto protoreflect.FileDescriptor

var file_api_v1_inference_server_management_proto_rawDesc = []byte{
	0x0a, 0x28, 0x61, 0x70, 0x69, 0x2f, 0x76, 0x31, 0x2f, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e,
	0x63, 0x65, 0x5f, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x5f, 0x6d, 0x61, 0x6e, 0x61, 0x67, 0x65,
	0x6d, 0x65, 0x6e, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x1d, 0x6c, 0x6c, 0x6d, 0x61,
	0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x2e,
	0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76, 0x31, 0x1a, 0x1c, 0x67, 0x6f, 0x6f, 0x67, 0x6c,
	0x65, 0x2f, 0x61, 0x70, 0x69, 0x2f, 0x61, 0x6e, 0x6e, 0x6f, 0x74, 0x61, 0x74, 0x69, 0x6f, 0x6e,
	0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xf9, 0x03, 0x0a, 0x0c, 0x45, 0x6e, 0x67, 0x69,
	0x6e, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x12, 0x1b, 0x0a, 0x09, 0x65, 0x6e, 0x67, 0x69,
	0x6e, 0x65, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x65, 0x6e, 0x67,
	0x69, 0x6e, 0x65, 0x49, 0x64, 0x12, 0x1f, 0x0a, 0x09, 0x6d, 0x6f, 0x64, 0x65, 0x6c, 0x5f, 0x69,
	0x64, 0x73, 0x18, 0x02, 0x20, 0x03, 0x28, 0x09, 0x42, 0x02, 0x18, 0x01, 0x52, 0x08, 0x6d, 0x6f,
	0x64, 0x65, 0x6c, 0x49, 0x64, 0x73, 0x12, 0x5b, 0x0a, 0x0b, 0x73, 0x79, 0x6e, 0x63, 0x5f, 0x73,
	0x74, 0x61, 0x74, 0x75, 0x73, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x36, 0x2e, 0x6c, 0x6c,
	0x6d, 0x61, 0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63,
	0x65, 0x2e, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76, 0x31, 0x2e, 0x45, 0x6e, 0x67, 0x69,
	0x6e, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x2e, 0x53, 0x79, 0x6e, 0x63, 0x53, 0x74, 0x61,
	0x74, 0x75, 0x73, 0x42, 0x02, 0x18, 0x01, 0x52, 0x0a, 0x73, 0x79, 0x6e, 0x63, 0x53, 0x74, 0x61,
	0x74, 0x75, 0x73, 0x12, 0x14, 0x0a, 0x05, 0x72, 0x65, 0x61, 0x64, 0x79, 0x18, 0x04, 0x20, 0x01,
	0x28, 0x08, 0x52, 0x05, 0x72, 0x65, 0x61, 0x64, 0x79, 0x12, 0x49, 0x0a, 0x06, 0x6d, 0x6f, 0x64,
	0x65, 0x6c, 0x73, 0x18, 0x05, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x31, 0x2e, 0x6c, 0x6c, 0x6d, 0x61,
	0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x2e,
	0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76, 0x31, 0x2e, 0x45, 0x6e, 0x67, 0x69, 0x6e, 0x65,
	0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x2e, 0x4d, 0x6f, 0x64, 0x65, 0x6c, 0x52, 0x06, 0x6d, 0x6f,
	0x64, 0x65, 0x6c, 0x73, 0x12, 0x1d, 0x0a, 0x0a, 0x63, 0x6c, 0x75, 0x73, 0x74, 0x65, 0x72, 0x5f,
	0x69, 0x64, 0x18, 0x06, 0x20, 0x01, 0x28, 0x09, 0x52, 0x09, 0x63, 0x6c, 0x75, 0x73, 0x74, 0x65,
	0x72, 0x49, 0x64, 0x1a, 0x3f, 0x0a, 0x0a, 0x53, 0x79, 0x6e, 0x63, 0x53, 0x74, 0x61, 0x74, 0x75,
	0x73, 0x12, 0x31, 0x0a, 0x15, 0x69, 0x6e, 0x5f, 0x70, 0x72, 0x6f, 0x67, 0x72, 0x65, 0x73, 0x73,
	0x5f, 0x6d, 0x6f, 0x64, 0x65, 0x6c, 0x5f, 0x69, 0x64, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x09,
	0x52, 0x12, 0x69, 0x6e, 0x50, 0x72, 0x6f, 0x67, 0x72, 0x65, 0x73, 0x73, 0x4d, 0x6f, 0x64, 0x65,
	0x6c, 0x49, 0x64, 0x73, 0x1a, 0x8c, 0x01, 0x0a, 0x05, 0x4d, 0x6f, 0x64, 0x65, 0x6c, 0x12, 0x0e,
	0x0a, 0x02, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x02, 0x69, 0x64, 0x12, 0x19,
	0x0a, 0x08, 0x69, 0x73, 0x5f, 0x72, 0x65, 0x61, 0x64, 0x79, 0x18, 0x02, 0x20, 0x01, 0x28, 0x08,
	0x52, 0x07, 0x69, 0x73, 0x52, 0x65, 0x61, 0x64, 0x79, 0x12, 0x33, 0x0a, 0x16, 0x69, 0x6e, 0x5f,
	0x70, 0x72, 0x6f, 0x67, 0x72, 0x65, 0x73, 0x73, 0x5f, 0x74, 0x61, 0x73, 0x6b, 0x5f, 0x63, 0x6f,
	0x75, 0x6e, 0x74, 0x18, 0x03, 0x20, 0x01, 0x28, 0x05, 0x52, 0x13, 0x69, 0x6e, 0x50, 0x72, 0x6f,
	0x67, 0x72, 0x65, 0x73, 0x73, 0x54, 0x61, 0x73, 0x6b, 0x43, 0x6f, 0x75, 0x6e, 0x74, 0x12, 0x23,
	0x0a, 0x0d, 0x67, 0x70, 0x75, 0x5f, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x61, 0x74, 0x65, 0x64, 0x18,
	0x04, 0x20, 0x01, 0x28, 0x05, 0x52, 0x0c, 0x67, 0x70, 0x75, 0x41, 0x6c, 0x6c, 0x6f, 0x63, 0x61,
	0x74, 0x65, 0x64, 0x22, 0xb6, 0x01, 0x0a, 0x0f, 0x49, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63,
	0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x12, 0x57, 0x0a, 0x10, 0x63, 0x6c, 0x75, 0x73, 0x74,
	0x65, 0x72, 0x5f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x65, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28,
	0x0b, 0x32, 0x2c, 0x2e, 0x6c, 0x6c, 0x6d, 0x61, 0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e,
	0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x2e, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76,
	0x31, 0x2e, 0x43, 0x6c, 0x75, 0x73, 0x74, 0x65, 0x72, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x52,
	0x0f, 0x63, 0x6c, 0x75, 0x73, 0x74, 0x65, 0x72, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x65, 0x73,
	0x12, 0x4a, 0x0a, 0x0b, 0x74, 0x61, 0x73, 0x6b, 0x5f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x29, 0x2e, 0x6c, 0x6c, 0x6d, 0x61, 0x72, 0x69, 0x6e, 0x65,
	0x72, 0x2e, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x2e, 0x73, 0x65, 0x72, 0x76,
	0x65, 0x72, 0x2e, 0x76, 0x31, 0x2e, 0x54, 0x61, 0x73, 0x6b, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73,
	0x52, 0x0a, 0x74, 0x61, 0x73, 0x6b, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x22, 0xd1, 0x01, 0x0a,
	0x0a, 0x54, 0x61, 0x73, 0x6b, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x12, 0x7a, 0x0a, 0x17, 0x69,
	0x6e, 0x5f, 0x70, 0x72, 0x6f, 0x67, 0x72, 0x65, 0x73, 0x73, 0x5f, 0x74, 0x61, 0x73, 0x6b, 0x5f,
	0x63, 0x6f, 0x75, 0x6e, 0x74, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x43, 0x2e, 0x6c,
	0x6c, 0x6d, 0x61, 0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e,
	0x63, 0x65, 0x2e, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76, 0x31, 0x2e, 0x54, 0x61, 0x73,
	0x6b, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x2e, 0x49, 0x6e, 0x50, 0x72, 0x6f, 0x67, 0x72, 0x65,
	0x73, 0x73, 0x54, 0x61, 0x73, 0x6b, 0x43, 0x6f, 0x75, 0x6e, 0x74, 0x73, 0x45, 0x6e, 0x74, 0x72,
	0x79, 0x52, 0x14, 0x69, 0x6e, 0x50, 0x72, 0x6f, 0x67, 0x72, 0x65, 0x73, 0x73, 0x54, 0x61, 0x73,
	0x6b, 0x43, 0x6f, 0x75, 0x6e, 0x74, 0x73, 0x1a, 0x47, 0x0a, 0x19, 0x49, 0x6e, 0x50, 0x72, 0x6f,
	0x67, 0x72, 0x65, 0x73, 0x73, 0x54, 0x61, 0x73, 0x6b, 0x43, 0x6f, 0x75, 0x6e, 0x74, 0x73, 0x45,
	0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x05, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01,
	0x22, 0x84, 0x02, 0x0a, 0x0d, 0x43, 0x6c, 0x75, 0x73, 0x74, 0x65, 0x72, 0x53, 0x74, 0x61, 0x74,
	0x75, 0x73, 0x12, 0x0e, 0x0a, 0x02, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x02,
	0x69, 0x64, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12, 0x54, 0x0a, 0x0f, 0x65, 0x6e, 0x67, 0x69, 0x6e, 0x65,
	0x5f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x65, 0x73, 0x18, 0x03, 0x20, 0x03, 0x28, 0x0b, 0x32,
	0x2b, 0x2e, 0x6c, 0x6c, 0x6d, 0x61, 0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e, 0x66, 0x65,
	0x72, 0x65, 0x6e, 0x63, 0x65, 0x2e, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76, 0x31, 0x2e,
	0x45, 0x6e, 0x67, 0x69, 0x6e, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x52, 0x0e, 0x65, 0x6e,
	0x67, 0x69, 0x6e, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x65, 0x73, 0x12, 0x1f, 0x0a, 0x0b,
	0x6d, 0x6f, 0x64, 0x65, 0x6c, 0x5f, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x18, 0x04, 0x20, 0x01, 0x28,
	0x05, 0x52, 0x0a, 0x6d, 0x6f, 0x64, 0x65, 0x6c, 0x43, 0x6f, 0x75, 0x6e, 0x74, 0x12, 0x33, 0x0a,
	0x16, 0x69, 0x6e, 0x5f, 0x70, 0x72, 0x6f, 0x67, 0x72, 0x65, 0x73, 0x73, 0x5f, 0x74, 0x61, 0x73,
	0x6b, 0x5f, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x18, 0x05, 0x20, 0x01, 0x28, 0x05, 0x52, 0x13, 0x69,
	0x6e, 0x50, 0x72, 0x6f, 0x67, 0x72, 0x65, 0x73, 0x73, 0x54, 0x61, 0x73, 0x6b, 0x43, 0x6f, 0x75,
	0x6e, 0x74, 0x12, 0x23, 0x0a, 0x0d, 0x67, 0x70, 0x75, 0x5f, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x61,
	0x74, 0x65, 0x64, 0x18, 0x06, 0x20, 0x01, 0x28, 0x05, 0x52, 0x0c, 0x67, 0x70, 0x75, 0x41, 0x6c,
	0x6c, 0x6f, 0x63, 0x61, 0x74, 0x65, 0x64, 0x22, 0x1b, 0x0a, 0x19, 0x47, 0x65, 0x74, 0x49, 0x6e,
	0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x52, 0x65, 0x71,
	0x75, 0x65, 0x73, 0x74, 0x32, 0xb1, 0x01, 0x0a, 0x10, 0x49, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e,
	0x63, 0x65, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x12, 0x9c, 0x01, 0x0a, 0x12, 0x47, 0x65,
	0x74, 0x49, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73,
	0x12, 0x38, 0x2e, 0x6c, 0x6c, 0x6d, 0x61, 0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e, 0x66,
	0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x2e, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76, 0x31,
	0x2e, 0x47, 0x65, 0x74, 0x49, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x53, 0x74, 0x61,
	0x74, 0x75, 0x73, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x2e, 0x2e, 0x6c, 0x6c, 0x6d,
	0x61, 0x72, 0x69, 0x6e, 0x65, 0x72, 0x2e, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65,
	0x2e, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2e, 0x76, 0x31, 0x2e, 0x49, 0x6e, 0x66, 0x65, 0x72,
	0x65, 0x6e, 0x63, 0x65, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x22, 0x1c, 0x82, 0xd3, 0xe4, 0x93,
	0x02, 0x16, 0x12, 0x14, 0x2f, 0x76, 0x31, 0x2f, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63,
	0x65, 0x2f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x42, 0x2f, 0x5a, 0x2d, 0x67, 0x69, 0x74, 0x68,
	0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x6c, 0x6c, 0x6d, 0x61, 0x72, 0x69, 0x6e, 0x65, 0x72,
	0x2f, 0x69, 0x6e, 0x66, 0x65, 0x72, 0x65, 0x6e, 0x63, 0x65, 0x2d, 0x6d, 0x61, 0x6e, 0x61, 0x67,
	0x65, 0x72, 0x2f, 0x61, 0x70, 0x69, 0x2f, 0x76, 0x31, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x33,
}

var (
	file_api_v1_inference_server_management_proto_rawDescOnce sync.Once
	file_api_v1_inference_server_management_proto_rawDescData = file_api_v1_inference_server_management_proto_rawDesc
)

func file_api_v1_inference_server_management_proto_rawDescGZIP() []byte {
	file_api_v1_inference_server_management_proto_rawDescOnce.Do(func() {
		file_api_v1_inference_server_management_proto_rawDescData = protoimpl.X.CompressGZIP(file_api_v1_inference_server_management_proto_rawDescData)
	})
	return file_api_v1_inference_server_management_proto_rawDescData
}

var file_api_v1_inference_server_management_proto_msgTypes = make([]protoimpl.MessageInfo, 8)
var file_api_v1_inference_server_management_proto_goTypes = []interface{}{
	(*EngineStatus)(nil),              // 0: llmariner.inference.server.v1.EngineStatus
	(*InferenceStatus)(nil),           // 1: llmariner.inference.server.v1.InferenceStatus
	(*TaskStatus)(nil),                // 2: llmariner.inference.server.v1.TaskStatus
	(*ClusterStatus)(nil),             // 3: llmariner.inference.server.v1.ClusterStatus
	(*GetInferenceStatusRequest)(nil), // 4: llmariner.inference.server.v1.GetInferenceStatusRequest
	(*EngineStatus_SyncStatus)(nil),   // 5: llmariner.inference.server.v1.EngineStatus.SyncStatus
	(*EngineStatus_Model)(nil),        // 6: llmariner.inference.server.v1.EngineStatus.Model
	nil,                               // 7: llmariner.inference.server.v1.TaskStatus.InProgressTaskCountsEntry
}
var file_api_v1_inference_server_management_proto_depIdxs = []int32{
	5, // 0: llmariner.inference.server.v1.EngineStatus.sync_status:type_name -> llmariner.inference.server.v1.EngineStatus.SyncStatus
	6, // 1: llmariner.inference.server.v1.EngineStatus.models:type_name -> llmariner.inference.server.v1.EngineStatus.Model
	3, // 2: llmariner.inference.server.v1.InferenceStatus.cluster_statuses:type_name -> llmariner.inference.server.v1.ClusterStatus
	2, // 3: llmariner.inference.server.v1.InferenceStatus.task_status:type_name -> llmariner.inference.server.v1.TaskStatus
	7, // 4: llmariner.inference.server.v1.TaskStatus.in_progress_task_counts:type_name -> llmariner.inference.server.v1.TaskStatus.InProgressTaskCountsEntry
	0, // 5: llmariner.inference.server.v1.ClusterStatus.engine_statuses:type_name -> llmariner.inference.server.v1.EngineStatus
	4, // 6: llmariner.inference.server.v1.InferenceService.GetInferenceStatus:input_type -> llmariner.inference.server.v1.GetInferenceStatusRequest
	1, // 7: llmariner.inference.server.v1.InferenceService.GetInferenceStatus:output_type -> llmariner.inference.server.v1.InferenceStatus
	7, // [7:8] is the sub-list for method output_type
	6, // [6:7] is the sub-list for method input_type
	6, // [6:6] is the sub-list for extension type_name
	6, // [6:6] is the sub-list for extension extendee
	0, // [0:6] is the sub-list for field type_name
}

func init() { file_api_v1_inference_server_management_proto_init() }
func file_api_v1_inference_server_management_proto_init() {
	if File_api_v1_inference_server_management_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_api_v1_inference_server_management_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*EngineStatus); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_api_v1_inference_server_management_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*InferenceStatus); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_api_v1_inference_server_management_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*TaskStatus); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_api_v1_inference_server_management_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ClusterStatus); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_api_v1_inference_server_management_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*GetInferenceStatusRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_api_v1_inference_server_management_proto_msgTypes[5].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*EngineStatus_SyncStatus); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_api_v1_inference_server_management_proto_msgTypes[6].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*EngineStatus_Model); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_api_v1_inference_server_management_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   8,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_api_v1_inference_server_management_proto_goTypes,
		DependencyIndexes: file_api_v1_inference_server_management_proto_depIdxs,
		MessageInfos:      file_api_v1_inference_server_management_proto_msgTypes,
	}.Build()
	File_api_v1_inference_server_management_proto = out.File
	file_api_v1_inference_server_management_proto_rawDesc = nil
	file_api_v1_inference_server_management_proto_goTypes = nil
	file_api_v1_inference_server_management_proto_depIdxs = nil
}
