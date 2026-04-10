declare module 'jmuxer' {
  interface JMuxerOptions {
    node: HTMLVideoElement;
    mode: 'video' | 'audio' | 'both';
    fps?: number;
    flushingTime?: number;
    debug?: boolean;
  }
  interface FeedData {
    video?: Uint8Array;
    audio?: Uint8Array;
  }
  export default class JMuxer {
    constructor(options: JMuxerOptions);
    feed(data: FeedData): void;
    destroy(): void;
  }
}
