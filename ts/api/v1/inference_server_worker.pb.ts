/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

import * as LlmarinerChatServerV1Inference_server from "./inference_server.pb"
import * as LlmarinerEmbeddingsServerV1Inference_server_embeddings from "./inference_server_embeddings.pb"

type Absent<T, K extends keyof T> = { [k in Exclude<keyof T, K>]?: undefined };
type OneOf<T> =
  | { [k in keyof T]?: undefined }
  | (
    keyof T extends infer K ?
      (K extends string & keyof T ? { [k in K]: T[K] } & Absent<T, K>
        : never)
    : never);
export type EngineStatusSyncStatus = {
  inProgressModelIds?: string[]
}

export type EngineStatusModel = {
  id?: string
  isReady?: boolean
  inProgressTaskCount?: number
  gpuAllocated?: number
}

export type EngineStatus = {
  engineId?: string
  modelIds?: string[]
  syncStatus?: EngineStatusSyncStatus
  ready?: boolean
  models?: EngineStatusModel[]
  clusterId?: string
}

export type HeaderValue = {
  values?: string[]
}

export type HttpResponse = {
  statusCode?: number
  status?: string
  header?: {[key: string]: HeaderValue}
  body?: Uint8Array
}

export type ServerSentEvent = {
  data?: Uint8Array
  isLastEvent?: boolean
}


type BaseTaskResult = {
  taskId?: string
}

export type TaskResult = BaseTaskResult
  & OneOf<{ httpResponse: HttpResponse; serverSentEvent: ServerSentEvent }>


type BaseProcessTasksRequest = {
}

export type ProcessTasksRequest = BaseProcessTasksRequest
  & OneOf<{ engineStatus: EngineStatus; taskResult: TaskResult }>


type BaseTaskRequest = {
}

export type TaskRequest = BaseTaskRequest
  & OneOf<{ chatCompletion: LlmarinerChatServerV1Inference_server.CreateChatCompletionRequest; embedding: LlmarinerEmbeddingsServerV1Inference_server_embeddings.CreateEmbeddingRequest }>

export type Task = {
  id?: string
  request?: TaskRequest
  header?: {[key: string]: HeaderValue}
}

export type ProcessTasksResponse = {
  newTask?: Task
}

export class InferenceWorkerService {
}