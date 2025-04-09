/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

import * as LlmarinerInferenceServerV1Inference_server_management from "./inference_server_management.pb"
import * as LlmarinerInferenceServerV1Inference_server_worker from "./inference_server_worker.pb"

type Absent<T, K extends keyof T> = { [k in Exclude<keyof T, K>]?: undefined };
type OneOf<T> =
  | { [k in keyof T]?: undefined }
  | (
    keyof T extends infer K ?
      (K extends string & keyof T ? { [k in K]: T[K] } & Absent<T, K>
        : never)
    : never);
export type ServerStatusEngineStatusWithTenantID = {
  engine_status?: LlmarinerInferenceServerV1Inference_server_management.EngineStatus
  tenant_id?: string
}

export type ServerStatus = {
  pod_name?: string
  engine_statuses?: ServerStatusEngineStatusWithTenantID[]
}


type BaseProcessTasksInternalRequest = {
}

export type ProcessTasksInternalRequest = BaseProcessTasksInternalRequest
  & OneOf<{ server_status: ServerStatus; task_result: LlmarinerInferenceServerV1Inference_server_worker.TaskResult }>

export type ProcessTasksInternalResponse = {
  new_task?: LlmarinerInferenceServerV1Inference_server_worker.Task
  tenant_id?: string
}

export class InferenceInternalService {
}