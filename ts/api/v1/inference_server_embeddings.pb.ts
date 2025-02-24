/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/
export type CreateEmbeddingRequest = {
  input?: string
  encoded_input?: string
  model?: string
  encoding_format?: string
  dimensions?: number
  user?: string
}

export type Embedding = {
  index?: number
  embedding?: number[]
  object?: string
}

export type EmbeddingsUsage = {
  prompt_tokens?: number
  total_tokens?: number
}

export type Embeddings = {
  object?: string
  data?: Embedding[]
  model?: string
  usage?: EmbeddingsUsage
}