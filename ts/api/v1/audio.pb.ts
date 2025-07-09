/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/

import * as fm from "../../fetch.pb"
export type CreateTranscriptionRequest = {
}

export type CreateTranscriptionResponse = {
}

export class AudioService {
  static CreateTranscription(req: CreateTranscriptionRequest, initReq?: fm.InitReq): Promise<CreateTranscriptionResponse> {
    return fm.fetchReq<CreateTranscriptionRequest, CreateTranscriptionResponse>(`/v1/audio/transcriptions`, {...initReq, method: "POST", body: JSON.stringify(req)})
  }
}