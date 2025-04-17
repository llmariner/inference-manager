import * as fm from "../../fetch.pb";
export type EngineStatusSyncStatus = {
    in_progress_model_ids?: string[];
};
export type EngineStatusModel = {
    id?: string;
    is_ready?: boolean;
    in_progress_task_count?: number;
    gpu_allocated?: number;
};
export type EngineStatus = {
    engine_id?: string;
    model_ids?: string[];
    sync_status?: EngineStatusSyncStatus;
    ready?: boolean;
    models?: EngineStatusModel[];
    cluster_id?: string;
};
export type InferenceStatus = {
    cluster_statuses?: ClusterStatus[];
    task_status?: TaskStatus;
};
export type TaskStatus = {
    in_progress_task_counts?: {
        [key: string]: number;
    };
};
export type ClusterStatus = {
    id?: string;
    name?: string;
    engine_statuses?: EngineStatus[];
    model_count?: number;
    in_progress_task_count?: number;
    gpu_allocated?: number;
};
export type GetInferenceStatusRequest = {};
export type ActivateModelRequest = {
    id?: string;
};
export type ActivateModelResponse = {};
export type DeactivateModelRequest = {
    id?: string;
};
export type DeactivateModelResponse = {};
export declare class InferenceService {
    static GetInferenceStatus(req: GetInferenceStatusRequest, initReq?: fm.InitReq): Promise<InferenceStatus>;
    static ActivateModel(req: ActivateModelRequest, initReq?: fm.InitReq): Promise<ActivateModelResponse>;
    static DeactivateModel(req: DeactivateModelRequest, initReq?: fm.InitReq): Promise<DeactivateModelResponse>;
}
