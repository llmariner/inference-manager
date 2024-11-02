export type CreateEmbeddingRequest = {
    input?: string;
    model?: string;
    encodingFormat?: string;
    dimensions?: number;
    user?: string;
};
export type Embedding = {
    index?: number;
    embedding?: number[];
    object?: string;
};
export type EmbeddingsUsage = {
    promptTokens?: number;
    totalTokens?: number;
};
export type Embeddings = {
    object?: string;
    data?: Embedding[];
    model?: string;
    usage?: EmbeddingsUsage;
};
