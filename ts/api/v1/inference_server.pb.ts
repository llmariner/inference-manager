/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/
export type CreateChatCompletionRequestMessageToolCallFunction = {
  name?: string
  arguments?: string
}

export type CreateChatCompletionRequestMessageToolCall = {
  id?: string
  type?: string
  function?: CreateChatCompletionRequestMessageToolCallFunction
}

export type CreateChatCompletionRequestMessageContentImageUrl = {
  url?: string
  detail?: string
}

export type CreateChatCompletionRequestMessageContentInputAudio = {
  data?: string
  format?: string
}

export type CreateChatCompletionRequestMessageContentAudioUrl = {
  url?: string
}

export type CreateChatCompletionRequestMessageContent = {
  type?: string
  text?: string
  image_url?: CreateChatCompletionRequestMessageContentImageUrl
  input_audio?: CreateChatCompletionRequestMessageContentInputAudio
  audio_url?: CreateChatCompletionRequestMessageContentAudioUrl
}

export type CreateChatCompletionRequestMessage = {
  content?: CreateChatCompletionRequestMessageContent[]
  role?: string
  name?: string
  tool_calls?: CreateChatCompletionRequestMessageToolCall[]
  tool_call_id?: string
}

export type CreateChatCompletionRequestToolChoiceFunction = {
  name?: string
}

export type CreateChatCompletionRequestToolChoice = {
  type?: string
  function?: CreateChatCompletionRequestToolChoiceFunction
}

export type CreateChatCompletionRequestResponseFormat = {
  type?: string
}

export type CreateChatCompletionRequestToolFunction = {
  description?: string
  name?: string
  encoded_parameters?: string
}

export type CreateChatCompletionRequestTool = {
  type?: string
  function?: CreateChatCompletionRequestToolFunction
}

export type CreateChatCompletionRequestStreamOptions = {
  include_usage?: boolean
}

export type CreateChatCompletionRequest = {
  messages?: CreateChatCompletionRequestMessage[]
  model?: string
  frequency_penalty?: number
  logit_bias?: {[key: string]: number}
  logprobs?: boolean
  top_logprobs?: number
  max_tokens?: number
  n?: number
  presence_penalty?: number
  response_format?: CreateChatCompletionRequestResponseFormat
  seed?: number
  stop?: string[]
  stream?: boolean
  stream_options?: CreateChatCompletionRequestStreamOptions
  temperature?: number
  top_p?: number
  tools?: CreateChatCompletionRequestTool[]
  tool_choice?: string
  tool_choice_object?: CreateChatCompletionRequestToolChoice
  user?: string
  max_completion_tokens?: number
  encoded_chat_template_kwargs?: string
}

export type ToolCallFunction = {
  name?: string
  arguments?: string
}

export type ToolCall = {
  id?: string
  type?: string
  function?: ToolCallFunction
}

export type LogprobsContentTopLogprobs = {
  token?: string
  logprob?: number
  bytes?: Uint8Array
}

export type LogprobsContent = {
  token?: string
  logprob?: number
  bytes?: Uint8Array
  top_logprobs?: LogprobsContentTopLogprobs
}

export type Logprobs = {
  content?: LogprobsContent[]
}

export type Usage = {
  completion_tokens?: number
  prompt_tokens?: number
  total_tokens?: number
}

export type ChatCompletionChoiceMessage = {
  content?: string
  tool_calls?: ToolCall[]
  role?: string
}

export type ChatCompletionChoice = {
  finish_reason?: string
  index?: number
  message?: ChatCompletionChoiceMessage
  logprobs?: Logprobs
}

export type ChatCompletion = {
  id?: string
  choices?: ChatCompletionChoice[]
  created?: number
  model?: string
  system_fingerprint?: string
  object?: string
  usage?: Usage
}

export type ChatCompletionChunkChoiceDeltaToolCallFunction = {
  name?: string
  arguments?: string
}

export type ChatCompletionChunkChoiceDeltaToolCall = {
  id?: string
  type?: string
  function?: ChatCompletionChunkChoiceDeltaToolCallFunction
}

export type ChatCompletionChunkChoiceDelta = {
  content?: string
  tool_calls?: ChatCompletionChunkChoiceDeltaToolCall[]
  role?: string
}

export type ChatCompletionChunkChoice = {
  delta?: ChatCompletionChunkChoiceDelta
  finish_reason?: string
  index?: number
  logprobs?: Logprobs
}

export type ChatCompletionChunk = {
  id?: string
  choices?: ChatCompletionChunkChoice[]
  created?: number
  model?: string
  system_fingerprint?: string
  object?: string
  usage?: Usage
}

export type RagFunction = {
  vector_store_name?: string
}

export type CreateCompletionRequestStreamOption = {
  include_usage?: boolean
}

export type CreateCompletionRequest = {
  model?: string
  prompt?: string
  best_of?: number
  echo?: boolean
  frequency_penalty?: number
  logit_bias?: {[key: string]: number}
  logprobs?: number
  max_tokens?: number
  n?: number
  presence_penalty?: number
  seed?: number
  stop?: string[]
  stream?: boolean
  stream_option?: CreateCompletionRequestStreamOption
  suffix?: string
  temperature?: number
  is_temperature_set?: boolean
  top_p?: number
  user?: string
}

export type CompletionChoiceLogprobs = {
  text_offset?: number
  token_logprobs?: number
  tokens?: string
  top_logprobs?: number
}

export type CompletionChoice = {
  finish_reason?: string
  index?: number
  logprobs?: CompletionChoiceLogprobs
  text?: string
}

export type Completion = {
  id?: string
  choices?: CompletionChoice[]
  created?: number
  model?: string
  system_fingerprint?: string
  object?: string
  usage?: Usage
}

export class ChatService {
}