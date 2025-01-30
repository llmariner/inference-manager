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
  imageUrl?: CreateChatCompletionRequestMessageContentImageUrl
  inputAudio?: CreateChatCompletionRequestMessageContentInputAudio
  audioUrl?: CreateChatCompletionRequestMessageContentAudioUrl
}

export type CreateChatCompletionRequestMessage = {
  content?: CreateChatCompletionRequestMessageContent[]
  role?: string
  name?: string
  toolCalls?: CreateChatCompletionRequestMessageToolCall[]
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
  encodedParameters?: string
}

export type CreateChatCompletionRequestTool = {
  type?: string
  function?: CreateChatCompletionRequestToolFunction
}

export type CreateChatCompletionRequestStreamOptions = {
  includeUsage?: boolean
}

export type CreateChatCompletionRequest = {
  messages?: CreateChatCompletionRequestMessage[]
  model?: string
  frequencyPenalty?: number
  logitBias?: {[key: string]: number}
  logprobs?: boolean
  topLogprobs?: number
  maxTokens?: number
  n?: number
  presencePenalty?: number
  responseFormat?: CreateChatCompletionRequestResponseFormat
  seed?: number
  stop?: string[]
  stream?: boolean
  streamOptions?: CreateChatCompletionRequestStreamOptions
  temperature?: number
  topP?: number
  tools?: CreateChatCompletionRequestTool[]
  toolChoice?: string
  toolChoiceObject?: CreateChatCompletionRequestToolChoice
  user?: string
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
  topLogprobs?: LogprobsContentTopLogprobs
}

export type Logprobs = {
  content?: LogprobsContent[]
}

export type Usage = {
  completionTokens?: number
  promptTokens?: number
  totalTokens?: number
}

export type ChatCompletionChoiceMessage = {
  content?: string
  toolCalls?: ToolCall[]
  role?: string
}

export type ChatCompletionChoice = {
  finishReason?: string
  index?: number
  message?: ChatCompletionChoiceMessage
  logprobs?: Logprobs
}

export type ChatCompletion = {
  id?: string
  choices?: ChatCompletionChoice[]
  created?: number
  model?: string
  systemFingerprint?: string
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
  toolCalls?: ChatCompletionChunkChoiceDeltaToolCall[]
  role?: string
}

export type ChatCompletionChunkChoice = {
  delta?: ChatCompletionChunkChoiceDelta
  finishReason?: string
  index?: number
  logprobs?: Logprobs
}

export type ChatCompletionChunk = {
  id?: string
  choices?: ChatCompletionChunkChoice[]
  created?: number
  model?: string
  systemFingerprint?: string
  object?: string
  usage?: Usage
}

export type RagFunction = {
  vectorStoreName?: string
}

export type CreateCompletionRequestStreamOption = {
  includeUsage?: boolean
}

export type CreateCompletionRequest = {
  model?: string
  prompt?: string
  bestOf?: number
  echo?: boolean
  frequencyPenalty?: number
  logitBias?: {[key: string]: number}
  logprobs?: number
  maxTokens?: number
  n?: number
  presencePenalty?: number
  seed?: number
  stop?: string[]
  stream?: boolean
  streamOption?: CreateCompletionRequestStreamOption
  suffix?: string
  temperature?: number
  topP?: number
  user?: string
}

export type CompletionChoiceLogprobs = {
  textOffset?: number
  tokenLogprobs?: number
  tokens?: string
  topLogprobs?: number
}

export type CompletionChoice = {
  finishReason?: string
  index?: number
  logprobs?: CompletionChoiceLogprobs
  text?: string
}

export type Completion = {
  id?: string
  choices?: CompletionChoice[]
  created?: number
  model?: string
  systemFingerprint?: string
  object?: string
  usage?: Usage
}

export class ChatService {
}