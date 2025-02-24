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
  in_progress_model_ids?: string[]
}

export type EngineStatusModel = {
  id?: string
  is_ready?: boolean
  in_progress_task_count?: number
  gpu_allocated?: number
}

export type EngineStatus = {
  engine_id?: string
  model_ids?: string[]
  sync_status?: EngineStatusSyncStatus
  ready?: boolean
  models?: EngineStatusModel[]
  cluster_id?: string
}

export type HeaderValue = {
  values?: string[]
}

export type HttpResponse = {
  status_code?: number
  status?: string
  header?: {[key: string]: HeaderValue}
  body?: Uint8Array
}

export type ServerSentEvent = {
  data?: Uint8Array
  is_last_event?: boolean
}


type BaseTaskResult = {
  task_id?: string
}

export type TaskResult = BaseTaskResult
  & OneOf<{ http_response: HttpResponse; server_sent_event: ServerSentEvent }>


type BaseProcessTasksRequest = {
}

export type ProcessTasksRequest = BaseProcessTasksRequest
  & OneOf<{ engine_status: EngineStatus; task_result: TaskResult }>


type BaseTaskRequest = {
}

export type TaskRequest = BaseTaskRequest
  & OneOf<{ chat_completion: LlmarinerChatServerV1Inference_server.CreateChatCompletionRequest; embedding: LlmarinerEmbeddingsServerV1Inference_server_embeddings.CreateEmbeddingRequest }>

export type Task = {
  id?: string
  request?: TaskRequest
  header?: {[key: string]: HeaderValue}
}

export type ProcessTasksResponse = {
  new_task?: Task
}

export class InferenceWorkerService {
}