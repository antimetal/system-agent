// Antimetal API definitions
// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code and APIs are governed by a source available license that can be found in
// the LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        (unknown)
// source: service/resource/v1/intake.proto

package resourcev1

import (
	v1 "github.com/antimetal/agent/pkg/api/resource/v1"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// DeltaOperation is the type of change that is being made to the object.
type DeltaOperation int32

const (
	// Create a new object in the graph.
	DeltaOperation_DELTA_OPERATION_CREATE DeltaOperation = 0
	// Update an existing object in the graph.
	DeltaOperation_DELTA_OPERATION_UPDATE DeltaOperation = 1
	// Delete an object from the graph.
	DeltaOperation_DELTA_OPERATION_DELETE DeltaOperation = 2
	// Heartbeat is a special type of delta that is used to keep an object with a
	// TTL alive.
	DeltaOperation_DELTA_OPERATION_HEARTBEAT DeltaOperation = 3
)

// Enum value maps for DeltaOperation.
var (
	DeltaOperation_name = map[int32]string{
		0: "DELTA_OPERATION_CREATE",
		1: "DELTA_OPERATION_UPDATE",
		2: "DELTA_OPERATION_DELETE",
		3: "DELTA_OPERATION_HEARTBEAT",
	}
	DeltaOperation_value = map[string]int32{
		"DELTA_OPERATION_CREATE":    0,
		"DELTA_OPERATION_UPDATE":    1,
		"DELTA_OPERATION_DELETE":    2,
		"DELTA_OPERATION_HEARTBEAT": 3,
	}
)

func (x DeltaOperation) Enum() *DeltaOperation {
	p := new(DeltaOperation)
	*p = x
	return p
}

func (x DeltaOperation) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (DeltaOperation) Descriptor() protoreflect.EnumDescriptor {
	return file_service_resource_v1_intake_proto_enumTypes[0].Descriptor()
}

func (DeltaOperation) Type() protoreflect.EnumType {
	return &file_service_resource_v1_intake_proto_enumTypes[0]
}

func (x DeltaOperation) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use DeltaOperation.Descriptor instead.
func (DeltaOperation) EnumDescriptor() ([]byte, []int) {
	return file_service_resource_v1_intake_proto_rawDescGZIP(), []int{0}
}

type DeltaRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	ClientId      string                 `protobuf:"bytes,1,opt,name=client_id,json=clientId,proto3" json:"client_id,omitempty"`
	Deltas        []*Delta               `protobuf:"bytes,2,rep,name=deltas,proto3" json:"deltas,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *DeltaRequest) Reset() {
	*x = DeltaRequest{}
	mi := &file_service_resource_v1_intake_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *DeltaRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeltaRequest) ProtoMessage() {}

func (x *DeltaRequest) ProtoReflect() protoreflect.Message {
	mi := &file_service_resource_v1_intake_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use DeltaRequest.ProtoReflect.Descriptor instead.
func (*DeltaRequest) Descriptor() ([]byte, []int) {
	return file_service_resource_v1_intake_proto_rawDescGZIP(), []int{0}
}

func (x *DeltaRequest) GetClientId() string {
	if x != nil {
		return x.ClientId
	}
	return ""
}

func (x *DeltaRequest) GetDeltas() []*Delta {
	if x != nil {
		return x.Deltas
	}
	return nil
}

type DeltaResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *DeltaResponse) Reset() {
	*x = DeltaResponse{}
	mi := &file_service_resource_v1_intake_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *DeltaResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeltaResponse) ProtoMessage() {}

func (x *DeltaResponse) ProtoReflect() protoreflect.Message {
	mi := &file_service_resource_v1_intake_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use DeltaResponse.ProtoReflect.Descriptor instead.
func (*DeltaResponse) Descriptor() ([]byte, []int) {
	return file_service_resource_v1_intake_proto_rawDescGZIP(), []int{1}
}

// Delta is a set of graph objects that is being modified by operation op.
type Delta struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Op            DeltaOperation         `protobuf:"varint,1,opt,name=op,proto3,enum=service.resource.v1.DeltaOperation" json:"op,omitempty"`
	Objects       []*v1.Object           `protobuf:"bytes,2,rep,name=objects,proto3" json:"objects,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Delta) Reset() {
	*x = Delta{}
	mi := &file_service_resource_v1_intake_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Delta) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Delta) ProtoMessage() {}

func (x *Delta) ProtoReflect() protoreflect.Message {
	mi := &file_service_resource_v1_intake_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Delta.ProtoReflect.Descriptor instead.
func (*Delta) Descriptor() ([]byte, []int) {
	return file_service_resource_v1_intake_proto_rawDescGZIP(), []int{2}
}

func (x *Delta) GetOp() DeltaOperation {
	if x != nil {
		return x.Op
	}
	return DeltaOperation_DELTA_OPERATION_CREATE
}

func (x *Delta) GetObjects() []*v1.Object {
	if x != nil {
		return x.Objects
	}
	return nil
}

var File_service_resource_v1_intake_proto protoreflect.FileDescriptor

const file_service_resource_v1_intake_proto_rawDesc = "" +
	"\n" +
	" service/resource/v1/intake.proto\x12\x13service.resource.v1\x1a\x18resource/v1/object.proto\"_\n" +
	"\fDeltaRequest\x12\x1b\n" +
	"\tclient_id\x18\x01 \x01(\tR\bclientId\x122\n" +
	"\x06deltas\x18\x02 \x03(\v2\x1a.service.resource.v1.DeltaR\x06deltas\"\x0f\n" +
	"\rDeltaResponse\"k\n" +
	"\x05Delta\x123\n" +
	"\x02op\x18\x01 \x01(\x0e2#.service.resource.v1.DeltaOperationR\x02op\x12-\n" +
	"\aobjects\x18\x02 \x03(\v2\x13.resource.v1.ObjectR\aobjects*\x83\x01\n" +
	"\x0eDeltaOperation\x12\x1a\n" +
	"\x16DELTA_OPERATION_CREATE\x10\x00\x12\x1a\n" +
	"\x16DELTA_OPERATION_UPDATE\x10\x01\x12\x1a\n" +
	"\x16DELTA_OPERATION_DELETE\x10\x02\x12\x1d\n" +
	"\x19DELTA_OPERATION_HEARTBEAT\x10\x032c\n" +
	"\rIntakeService\x12R\n" +
	"\x05Delta\x12!.service.resource.v1.DeltaRequest\x1a\".service.resource.v1.DeltaResponse\"\x00(\x01B\xd7\x01\n" +
	"\x17com.service.resource.v1B\vIntakeProtoP\x01ZAgithub.com/antimetal/agent/pkg/api/service/resource/v1;resourcev1\xa2\x02\x03SRX\xaa\x02\x13Service.Resource.V1\xca\x02\x13Service\\Resource\\V1\xe2\x02\x1fService\\Resource\\V1\\GPBMetadata\xea\x02\x15Service::Resource::V1b\x06proto3"

var (
	file_service_resource_v1_intake_proto_rawDescOnce sync.Once
	file_service_resource_v1_intake_proto_rawDescData []byte
)

func file_service_resource_v1_intake_proto_rawDescGZIP() []byte {
	file_service_resource_v1_intake_proto_rawDescOnce.Do(func() {
		file_service_resource_v1_intake_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_service_resource_v1_intake_proto_rawDesc), len(file_service_resource_v1_intake_proto_rawDesc)))
	})
	return file_service_resource_v1_intake_proto_rawDescData
}

var file_service_resource_v1_intake_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_service_resource_v1_intake_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_service_resource_v1_intake_proto_goTypes = []any{
	(DeltaOperation)(0),   // 0: service.resource.v1.DeltaOperation
	(*DeltaRequest)(nil),  // 1: service.resource.v1.DeltaRequest
	(*DeltaResponse)(nil), // 2: service.resource.v1.DeltaResponse
	(*Delta)(nil),         // 3: service.resource.v1.Delta
	(*v1.Object)(nil),     // 4: resource.v1.Object
}
var file_service_resource_v1_intake_proto_depIdxs = []int32{
	3, // 0: service.resource.v1.DeltaRequest.deltas:type_name -> service.resource.v1.Delta
	0, // 1: service.resource.v1.Delta.op:type_name -> service.resource.v1.DeltaOperation
	4, // 2: service.resource.v1.Delta.objects:type_name -> resource.v1.Object
	1, // 3: service.resource.v1.IntakeService.Delta:input_type -> service.resource.v1.DeltaRequest
	2, // 4: service.resource.v1.IntakeService.Delta:output_type -> service.resource.v1.DeltaResponse
	4, // [4:5] is the sub-list for method output_type
	3, // [3:4] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_service_resource_v1_intake_proto_init() }
func file_service_resource_v1_intake_proto_init() {
	if File_service_resource_v1_intake_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_service_resource_v1_intake_proto_rawDesc), len(file_service_resource_v1_intake_proto_rawDesc)),
			NumEnums:      1,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_service_resource_v1_intake_proto_goTypes,
		DependencyIndexes: file_service_resource_v1_intake_proto_depIdxs,
		EnumInfos:         file_service_resource_v1_intake_proto_enumTypes,
		MessageInfos:      file_service_resource_v1_intake_proto_msgTypes,
	}.Build()
	File_service_resource_v1_intake_proto = out.File
	file_service_resource_v1_intake_proto_goTypes = nil
	file_service_resource_v1_intake_proto_depIdxs = nil
}
