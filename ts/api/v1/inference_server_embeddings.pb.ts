/* eslint-disable */
// @ts-nocheck
/*
* This file is a generated Typescript file for GRPC Gateway, DO NOT MODIFY
*/
export type CreateEmbeddingRequest = {
  input?: string
  model?: string
  encodingFormat?: string
  dimensions?: number
  user?: string
}

export type Embedding = {
  index?: number
  embedding?: number[]
  object?: string
}