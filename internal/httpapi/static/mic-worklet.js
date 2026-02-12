class SamanthaMicCaptureProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.chunkSize = 1024;
    this.pending = new Float32Array(this.chunkSize);
    this.pendingLen = 0;
  }

  flush() {
    if (this.pendingLen === 0) {
      return;
    }
    const out = this.pending.slice(0, this.pendingLen);
    this.pendingLen = 0;
    this.port.postMessage(
      {
        type: "audio-frame",
        sampleRate,
        samples: out.buffer,
      },
      [out.buffer],
    );
  }

  push(input) {
    if (!input || input.length === 0) {
      return;
    }
    let offset = 0;
    while (offset < input.length) {
      const avail = this.chunkSize - this.pendingLen;
      const remain = input.length - offset;
      const take = Math.min(avail, remain);
      this.pending.set(input.subarray(offset, offset + take), this.pendingLen);
      this.pendingLen += take;
      offset += take;
      if (this.pendingLen >= this.chunkSize) {
        this.flush();
      }
    }
  }

  process(inputs) {
    const input0 = inputs && inputs[0];
    const ch0 = input0 && input0[0];
    if (ch0 && ch0.length > 0) {
      this.push(ch0);
    }
    return true;
  }
}

registerProcessor("samantha-mic-capture", SamanthaMicCaptureProcessor);
