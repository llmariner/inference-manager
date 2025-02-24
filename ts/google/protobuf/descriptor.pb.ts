/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

export enum Edition {
  EDITION_UNKNOWN = "EDITION_UNKNOWN",
  EDITION_LEGACY = "EDITION_LEGACY",
  EDITION_PROTO2 = "EDITION_PROTO2",
  EDITION_PROTO3 = "EDITION_PROTO3",
  EDITION_2023 = "EDITION_2023",
  EDITION_2024 = "EDITION_2024",
  EDITION_1_TEST_ONLY = "EDITION_1_TEST_ONLY",
  EDITION_2_TEST_ONLY = "EDITION_2_TEST_ONLY",
  EDITION_99997_TEST_ONLY = "EDITION_99997_TEST_ONLY",
  EDITION_99998_TEST_ONLY = "EDITION_99998_TEST_ONLY",
  EDITION_99999_TEST_ONLY = "EDITION_99999_TEST_ONLY",
  EDITION_MAX = "EDITION_MAX",
}

export enum ExtensionRangeOptionsVerificationState {
  DECLARATION = "DECLARATION",
  UNVERIFIED = "UNVERIFIED",
}

export enum FieldDescriptorProtoType {
  TYPE_DOUBLE = "TYPE_DOUBLE",
  TYPE_FLOAT = "TYPE_FLOAT",
  TYPE_INT64 = "TYPE_INT64",
  TYPE_UINT64 = "TYPE_UINT64",
  TYPE_INT32 = "TYPE_INT32",
  TYPE_FIXED64 = "TYPE_FIXED64",
  TYPE_FIXED32 = "TYPE_FIXED32",
  TYPE_BOOL = "TYPE_BOOL",
  TYPE_STRING = "TYPE_STRING",
  TYPE_GROUP = "TYPE_GROUP",
  TYPE_MESSAGE = "TYPE_MESSAGE",
  TYPE_BYTES = "TYPE_BYTES",
  TYPE_UINT32 = "TYPE_UINT32",
  TYPE_ENUM = "TYPE_ENUM",
  TYPE_SFIXED32 = "TYPE_SFIXED32",
  TYPE_SFIXED64 = "TYPE_SFIXED64",
  TYPE_SINT32 = "TYPE_SINT32",
  TYPE_SINT64 = "TYPE_SINT64",
}

export enum FieldDescriptorProtoLabel {
  LABEL_OPTIONAL = "LABEL_OPTIONAL",
  LABEL_REPEATED = "LABEL_REPEATED",
  LABEL_REQUIRED = "LABEL_REQUIRED",
}

export enum FileOptionsOptimizeMode {
  SPEED = "SPEED",
  CODE_SIZE = "CODE_SIZE",
  LITE_RUNTIME = "LITE_RUNTIME",
}

export enum FieldOptionsCType {
  STRING = "STRING",
  CORD = "CORD",
  STRING_PIECE = "STRING_PIECE",
}

export enum FieldOptionsJSType {
  JS_NORMAL = "JS_NORMAL",
  JS_STRING = "JS_STRING",
  JS_NUMBER = "JS_NUMBER",
}

export enum FieldOptionsOptionRetention {
  RETENTION_UNKNOWN = "RETENTION_UNKNOWN",
  RETENTION_RUNTIME = "RETENTION_RUNTIME",
  RETENTION_SOURCE = "RETENTION_SOURCE",
}

export enum FieldOptionsOptionTargetType {
  TARGET_TYPE_UNKNOWN = "TARGET_TYPE_UNKNOWN",
  TARGET_TYPE_FILE = "TARGET_TYPE_FILE",
  TARGET_TYPE_EXTENSION_RANGE = "TARGET_TYPE_EXTENSION_RANGE",
  TARGET_TYPE_MESSAGE = "TARGET_TYPE_MESSAGE",
  TARGET_TYPE_FIELD = "TARGET_TYPE_FIELD",
  TARGET_TYPE_ONEOF = "TARGET_TYPE_ONEOF",
  TARGET_TYPE_ENUM = "TARGET_TYPE_ENUM",
  TARGET_TYPE_ENUM_ENTRY = "TARGET_TYPE_ENUM_ENTRY",
  TARGET_TYPE_SERVICE = "TARGET_TYPE_SERVICE",
  TARGET_TYPE_METHOD = "TARGET_TYPE_METHOD",
}

export enum MethodOptionsIdempotencyLevel {
  IDEMPOTENCY_UNKNOWN = "IDEMPOTENCY_UNKNOWN",
  NO_SIDE_EFFECTS = "NO_SIDE_EFFECTS",
  IDEMPOTENT = "IDEMPOTENT",
}

export enum FeatureSetFieldPresence {
  FIELD_PRESENCE_UNKNOWN = "FIELD_PRESENCE_UNKNOWN",
  EXPLICIT = "EXPLICIT",
  IMPLICIT = "IMPLICIT",
  LEGACY_REQUIRED = "LEGACY_REQUIRED",
}

export enum FeatureSetEnumType {
  ENUM_TYPE_UNKNOWN = "ENUM_TYPE_UNKNOWN",
  OPEN = "OPEN",
  CLOSED = "CLOSED",
}

export enum FeatureSetRepeatedFieldEncoding {
  REPEATED_FIELD_ENCODING_UNKNOWN = "REPEATED_FIELD_ENCODING_UNKNOWN",
  PACKED = "PACKED",
  EXPANDED = "EXPANDED",
}

export enum FeatureSetUtf8Validation {
  UTF8_VALIDATION_UNKNOWN = "UTF8_VALIDATION_UNKNOWN",
  VERIFY = "VERIFY",
  NONE = "NONE",
}

export enum FeatureSetMessageEncoding {
  MESSAGE_ENCODING_UNKNOWN = "MESSAGE_ENCODING_UNKNOWN",
  LENGTH_PREFIXED = "LENGTH_PREFIXED",
  DELIMITED = "DELIMITED",
}

export enum FeatureSetJsonFormat {
  JSON_FORMAT_UNKNOWN = "JSON_FORMAT_UNKNOWN",
  ALLOW = "ALLOW",
  LEGACY_BEST_EFFORT = "LEGACY_BEST_EFFORT",
}

export enum GeneratedCodeInfoAnnotationSemantic {
  NONE = "NONE",
  SET = "SET",
  ALIAS = "ALIAS",
}

export type FileDescriptorSet = {
  file?: FileDescriptorProto[]
}

export type FileDescriptorProto = {
  name?: string
  package?: string
  dependency?: string[]
  public_dependency?: number[]
  weak_dependency?: number[]
  message_type?: DescriptorProto[]
  enum_type?: EnumDescriptorProto[]
  service?: ServiceDescriptorProto[]
  extension?: FieldDescriptorProto[]
  options?: FileOptions
  source_code_info?: SourceCodeInfo
  syntax?: string
  edition?: Edition
}

export type DescriptorProtoExtensionRange = {
  start?: number
  end?: number
  options?: ExtensionRangeOptions
}

export type DescriptorProtoReservedRange = {
  start?: number
  end?: number
}

export type DescriptorProto = {
  name?: string
  field?: FieldDescriptorProto[]
  extension?: FieldDescriptorProto[]
  nested_type?: DescriptorProto[]
  enum_type?: EnumDescriptorProto[]
  extension_range?: DescriptorProtoExtensionRange[]
  oneof_decl?: OneofDescriptorProto[]
  options?: MessageOptions
  reserved_range?: DescriptorProtoReservedRange[]
  reserved_name?: string[]
}

export type ExtensionRangeOptionsDeclaration = {
  number?: number
  full_name?: string
  type?: string
  reserved?: boolean
  repeated?: boolean
}

export type ExtensionRangeOptions = {
  uninterpreted_option?: UninterpretedOption[]
  declaration?: ExtensionRangeOptionsDeclaration[]
  features?: FeatureSet
  verification?: ExtensionRangeOptionsVerificationState
}

export type FieldDescriptorProto = {
  name?: string
  number?: number
  label?: FieldDescriptorProtoLabel
  type?: FieldDescriptorProtoType
  type_name?: string
  extendee?: string
  default_value?: string
  oneof_index?: number
  json_name?: string
  options?: FieldOptions
  proto3_optional?: boolean
}

export type OneofDescriptorProto = {
  name?: string
  options?: OneofOptions
}

export type EnumDescriptorProtoEnumReservedRange = {
  start?: number
  end?: number
}

export type EnumDescriptorProto = {
  name?: string
  value?: EnumValueDescriptorProto[]
  options?: EnumOptions
  reserved_range?: EnumDescriptorProtoEnumReservedRange[]
  reserved_name?: string[]
}

export type EnumValueDescriptorProto = {
  name?: string
  number?: number
  options?: EnumValueOptions
}

export type ServiceDescriptorProto = {
  name?: string
  method?: MethodDescriptorProto[]
  options?: ServiceOptions
}

export type MethodDescriptorProto = {
  name?: string
  input_type?: string
  output_type?: string
  options?: MethodOptions
  client_streaming?: boolean
  server_streaming?: boolean
}

export type FileOptions = {
  java_package?: string
  java_outer_classname?: string
  java_multiple_files?: boolean
  java_generate_equals_and_hash?: boolean
  java_string_check_utf8?: boolean
  optimize_for?: FileOptionsOptimizeMode
  go_package?: string
  cc_generic_services?: boolean
  java_generic_services?: boolean
  py_generic_services?: boolean
  deprecated?: boolean
  cc_enable_arenas?: boolean
  objc_class_prefix?: string
  csharp_namespace?: string
  swift_prefix?: string
  php_class_prefix?: string
  php_namespace?: string
  php_metadata_namespace?: string
  ruby_package?: string
  features?: FeatureSet
  uninterpreted_option?: UninterpretedOption[]
}

export type MessageOptions = {
  message_set_wire_format?: boolean
  no_standard_descriptor_accessor?: boolean
  deprecated?: boolean
  map_entry?: boolean
  deprecated_legacy_json_field_conflicts?: boolean
  features?: FeatureSet
  uninterpreted_option?: UninterpretedOption[]
}

export type FieldOptionsEditionDefault = {
  edition?: Edition
  value?: string
}

export type FieldOptionsFeatureSupport = {
  edition_introduced?: Edition
  edition_deprecated?: Edition
  deprecation_warning?: string
  edition_removed?: Edition
}

export type FieldOptions = {
  ctype?: FieldOptionsCType
  packed?: boolean
  jstype?: FieldOptionsJSType
  lazy?: boolean
  unverified_lazy?: boolean
  deprecated?: boolean
  weak?: boolean
  debug_redact?: boolean
  retention?: FieldOptionsOptionRetention
  targets?: FieldOptionsOptionTargetType[]
  edition_defaults?: FieldOptionsEditionDefault[]
  features?: FeatureSet
  feature_support?: FieldOptionsFeatureSupport
  uninterpreted_option?: UninterpretedOption[]
}

export type OneofOptions = {
  features?: FeatureSet
  uninterpreted_option?: UninterpretedOption[]
}

export type EnumOptions = {
  allow_alias?: boolean
  deprecated?: boolean
  deprecated_legacy_json_field_conflicts?: boolean
  features?: FeatureSet
  uninterpreted_option?: UninterpretedOption[]
}

export type EnumValueOptions = {
  deprecated?: boolean
  features?: FeatureSet
  debug_redact?: boolean
  feature_support?: FieldOptionsFeatureSupport
  uninterpreted_option?: UninterpretedOption[]
}

export type ServiceOptions = {
  features?: FeatureSet
  deprecated?: boolean
  uninterpreted_option?: UninterpretedOption[]
}

export type MethodOptions = {
  deprecated?: boolean
  idempotency_level?: MethodOptionsIdempotencyLevel
  features?: FeatureSet
  uninterpreted_option?: UninterpretedOption[]
}

export type UninterpretedOptionNamePart = {
  name_part?: string
  is_extension?: boolean
}

export type UninterpretedOption = {
  name?: UninterpretedOptionNamePart[]
  identifier_value?: string
  positive_int_value?: string
  negative_int_value?: string
  double_value?: number
  string_value?: Uint8Array
  aggregate_value?: string
}

export type FeatureSet = {
  field_presence?: FeatureSetFieldPresence
  enum_type?: FeatureSetEnumType
  repeated_field_encoding?: FeatureSetRepeatedFieldEncoding
  utf8_validation?: FeatureSetUtf8Validation
  message_encoding?: FeatureSetMessageEncoding
  json_format?: FeatureSetJsonFormat
}

export type FeatureSetDefaultsFeatureSetEditionDefault = {
  edition?: Edition
  overridable_features?: FeatureSet
  fixed_features?: FeatureSet
}

export type FeatureSetDefaults = {
  defaults?: FeatureSetDefaultsFeatureSetEditionDefault[]
  minimum_edition?: Edition
  maximum_edition?: Edition
}

export type SourceCodeInfoLocation = {
  path?: number[]
  span?: number[]
  leading_comments?: string
  trailing_comments?: string
  leading_detached_comments?: string[]
}

export type SourceCodeInfo = {
  location?: SourceCodeInfoLocation[]
}

export type GeneratedCodeInfoAnnotation = {
  path?: number[]
  source_file?: string
  begin?: number
  end?: number
  semantic?: GeneratedCodeInfoAnnotationSemantic
}

export type GeneratedCodeInfo = {
  annotation?: GeneratedCodeInfoAnnotation[]
}