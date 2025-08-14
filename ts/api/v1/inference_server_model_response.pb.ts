/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/
export type CreateModelResponseRequestInputContent = {
  type?: string
  text?: string
  detail?: string
  image_url?: string
  file_id?: string
  file_data?: string
  file_url?: string
  filename?: string
}

export type CreateModelResponseRequestInput = {
  type?: string
  content?: CreateModelResponseRequestInputContent[]
  role?: string
  status?: string
  id?: string
}

export type CreateModelResponseRequestPrompt = {
  id?: string
  version?: string
}

export type CreateModelResponseRequestReasoning = {
  effort?: string
  generate_summary?: string
  summary?: string
}

export type CreateModelResponseRequestStreamOptions = {
  include_obfuscation?: boolean
}

export type CreateModelResponseRequestTextFormat = {
  type?: string
  name?: string
  encoded_schema?: string
  description?: string
  strict?: boolean
}

export type CreateModelResponseRequestText = {
  format?: CreateModelResponseRequestTextFormat
  verbosity?: string
}

export type CreateModelResponseRequestToolChoice = {
  type?: string
  name?: string
  mode?: string
  encoded_tools?: string
  server_label?: string
}

export type CreateModelResponseRequest = {
  background?: boolean
  include?: string[]
  input?: string
  encoded_input?: string
  instructions?: string
  max_output_tokens?: number
  max_tool_calls?: number
  metadata?: {[key: string]: string}
  model?: string
  parallel_tool_calls?: boolean
  previous_response_id?: string
  prompt?: CreateModelResponseRequestPrompt
  prompt_cache_key?: string
  reasoning?: CreateModelResponseRequestReasoning
  safety_identifier?: string
  service_tier?: string
  store?: boolean
  stream?: boolean
  stream_options?: CreateModelResponseRequestStreamOptions
  temperature?: number
  is_temperature_set?: boolean
  text?: CreateModelResponseRequestText
  tool_choice?: string
  tool_choice_object?: CreateModelResponseRequestToolChoice
  encoded_tools?: string
  top_logprobs?: number
  top_p?: number
  is_top_p_set?: boolean
  truncation?: boolean
  user?: string
}

export type ModelResponse = {
}