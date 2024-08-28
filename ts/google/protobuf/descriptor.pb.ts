/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

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
  LABEL_REQUIRED = "LABEL_REQUIRED",
  LABEL_REPEATED = "LABEL_REPEATED",
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

export enum MethodOptionsIdempotencyLevel {
  IDEMPOTENCY_UNKNOWN = "IDEMPOTENCY_UNKNOWN",
  NO_SIDE_EFFECTS = "NO_SIDE_EFFECTS",
  IDEMPOTENT = "IDEMPOTENT",
}

export type FileDescriptorSet = {
  file?: FileDescriptorProto[]
}

export type FileDescriptorProto = {
  name?: string
  package?: string
  dependency?: string[]
  publicDependency?: number[]
  weakDependency?: number[]
  messageType?: DescriptorProto[]
  enumType?: EnumDescriptorProto[]
  service?: ServiceDescriptorProto[]
  extension?: FieldDescriptorProto[]
  options?: FileOptions
  sourceCodeInfo?: SourceCodeInfo
  syntax?: string
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
  nestedType?: DescriptorProto[]
  enumType?: EnumDescriptorProto[]
  extensionRange?: DescriptorProtoExtensionRange[]
  oneofDecl?: OneofDescriptorProto[]
  options?: MessageOptions
  reservedRange?: DescriptorProtoReservedRange[]
  reservedName?: string[]
}

export type ExtensionRangeOptions = {
  uninterpretedOption?: UninterpretedOption[]
}

export type FieldDescriptorProto = {
  name?: string
  number?: number
  label?: FieldDescriptorProtoLabel
  type?: FieldDescriptorProtoType
  typeName?: string
  extendee?: string
  defaultValue?: string
  oneofIndex?: number
  jsonName?: string
  options?: FieldOptions
  proto3Optional?: boolean
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
  reservedRange?: EnumDescriptorProtoEnumReservedRange[]
  reservedName?: string[]
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
  inputType?: string
  outputType?: string
  options?: MethodOptions
  clientStreaming?: boolean
  serverStreaming?: boolean
}

export type FileOptions = {
  javaPackage?: string
  javaOuterClassname?: string
  javaMultipleFiles?: boolean
  javaGenerateEqualsAndHash?: boolean
  javaStringCheckUtf8?: boolean
  optimizeFor?: FileOptionsOptimizeMode
  goPackage?: string
  ccGenericServices?: boolean
  javaGenericServices?: boolean
  pyGenericServices?: boolean
  phpGenericServices?: boolean
  deprecated?: boolean
  ccEnableArenas?: boolean
  objcClassPrefix?: string
  csharpNamespace?: string
  swiftPrefix?: string
  phpClassPrefix?: string
  phpNamespace?: string
  phpMetadataNamespace?: string
  rubyPackage?: string
  uninterpretedOption?: UninterpretedOption[]
}

export type MessageOptions = {
  messageSetWireFormat?: boolean
  noStandardDescriptorAccessor?: boolean
  deprecated?: boolean
  mapEntry?: boolean
  uninterpretedOption?: UninterpretedOption[]
}

export type FieldOptions = {
  ctype?: FieldOptionsCType
  packed?: boolean
  jstype?: FieldOptionsJSType
  lazy?: boolean
  unverifiedLazy?: boolean
  deprecated?: boolean
  weak?: boolean
  uninterpretedOption?: UninterpretedOption[]
}

export type OneofOptions = {
  uninterpretedOption?: UninterpretedOption[]
}

export type EnumOptions = {
  allowAlias?: boolean
  deprecated?: boolean
  uninterpretedOption?: UninterpretedOption[]
}

export type EnumValueOptions = {
  deprecated?: boolean
  uninterpretedOption?: UninterpretedOption[]
}

export type ServiceOptions = {
  deprecated?: boolean
  uninterpretedOption?: UninterpretedOption[]
}

export type MethodOptions = {
  deprecated?: boolean
  idempotencyLevel?: MethodOptionsIdempotencyLevel
  uninterpretedOption?: UninterpretedOption[]
}

export type UninterpretedOptionNamePart = {
  namePart?: string
  isExtension?: boolean
}

export type UninterpretedOption = {
  name?: UninterpretedOptionNamePart[]
  identifierValue?: string
  positiveIntValue?: string
  negativeIntValue?: string
  doubleValue?: number
  stringValue?: Uint8Array
  aggregateValue?: string
}

export type SourceCodeInfoLocation = {
  path?: number[]
  span?: number[]
  leadingComments?: string
  trailingComments?: string
  leadingDetachedComments?: string[]
}

export type SourceCodeInfo = {
  location?: SourceCodeInfoLocation[]
}

export type GeneratedCodeInfoAnnotation = {
  path?: number[]
  sourceFile?: string
  begin?: number
  end?: number
}

export type GeneratedCodeInfo = {
  annotation?: GeneratedCodeInfoAnnotation[]
}