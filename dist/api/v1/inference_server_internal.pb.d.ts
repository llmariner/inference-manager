import * as LlmarinerInferenceServerV1Inference_server_worker from "./inference_server_worker.pb";
type Absent<T, K extends keyof T> = {
    [k in Exclude<keyof T, K>]?: undefined;
};
type OneOf<T> = {
    [k in keyof T]?: undefined;
} | (keyof T extends infer K ? (K extends string & keyof T ? {
    [k in K]: T[K];
} & Absent<T, K> : never) : never);
export type ServerStatusEngineStatusWithTenantID = {
    engineStatus?: LlmarinerInferenceServerV1Inference_server_worker.EngineStatus;
    tenantId?: string;
};
export type ServerStatus = {
    podName?: string;
    engineStatuses?: ServerStatusEngineStatusWithTenantID[];
};
type BaseProcessTasksInternalRequest = {};
export type ProcessTasksInternalRequest = BaseProcessTasksInternalRequest & OneOf<{
    serverStatus: ServerStatus;
    taskResult: LlmarinerInferenceServerV1Inference_server_worker.TaskResult;
}>;
export type ProcessTasksInternalResponse = {
    newTask?: LlmarinerInferenceServerV1Inference_server_worker.Task;
    tenantId?: string;
};
export declare class InferenceInternalService {
}
export {};
