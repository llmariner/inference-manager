/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/
export type CreateAudioTranscriptionRequest = {
  file?: Uint8Array
  filename?: string
  model?: string
  language?: string
  prompt?: string
  response_format?: string
  stream?: boolean
  temperature?: number
}

export type TranscriptionUsageInputTokenDetails = {
  audio_tokens?: number
  text_tokens?: number
}

export type TranscriptionUsage = {
  type?: string
  input_tokens?: number
  output_tokens?: number
  total_tokens?: number
  input_token_details?: TranscriptionUsageInputTokenDetails
  seconds?: number
}

export type Transcription = {
  text?: string
  usage?: TranscriptionUsage
}