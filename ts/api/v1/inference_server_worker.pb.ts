/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

import * as LlmarinerAudioServerV1Inference_server_audio from "./inference_server_audio.pb"
import * as LlmarinerChatServerV1Inference_server_chat from "./inference_server_chat.pb"
import * as LlmarinerEmbeddingsServerV1Inference_server_embeddings from "./inference_server_embeddings.pb"
import * as LlmarinerInferenceServerV1Inference_server_management from "./inference_server_management.pb"

type Absent<T, K extends keyof T> = { [k in Exclude<keyof T, K>]?: undefined };
type OneOf<T> =
  | { [k in keyof T]?: undefined }
  | (
    keyof T extends infer K ?
      (K extends string & keyof T ? { [k in K]: T[K] } & Absent<T, K>
        : never)
    : never);
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
  result_index?: number
}

export type TaskResult = BaseTaskResult
  & OneOf<{ http_response: HttpResponse; server_sent_event: ServerSentEvent }>


type BaseProcessTasksRequest = {
}

export type ProcessTasksRequest = BaseProcessTasksRequest
  & OneOf<{ engine_status: LlmarinerInferenceServerV1Inference_server_management.EngineStatus; task_result: TaskResult }>

export type GoAwayRequest = {
}

export type HeartbeatRequest = {
}


type BaseTaskRequest = {
}

export type TaskRequest = BaseTaskRequest
  & OneOf<{ chat_completion: LlmarinerChatServerV1Inference_server_chat.CreateChatCompletionRequest; embedding: LlmarinerEmbeddingsServerV1Inference_server_embeddings.CreateEmbeddingRequest; audio_transcription: LlmarinerAudioServerV1Inference_server_audio.CreateAudioTranscriptionRequest; go_away: GoAwayRequest; heartbeat: HeartbeatRequest }>

export type Task = {
  id?: string
  request?: TaskRequest
  header?: {[key: string]: HeaderValue}
  engine_id?: string
  timeout_seconds?: number
}

export type ProcessTasksResponse = {
  new_task?: Task
}

export class InferenceWorkerService {
}