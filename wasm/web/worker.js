// Runs the Go code (ifgrad.wasm) off the main thread so the page stays
// responsive while large images are converted.
importScripts('wasm_exec.js');

const ready = new Promise(resolve => { self.ifgradReady = resolve; });
const go = new Go();
WebAssembly.instantiateStreaming(fetch('ifgrad.wasm'), go.importObject)
  .then(r => go.run(r.instance))
  .catch(err => postMessage({id: 0, error: `Could not load ifgrad.wasm: ${err}`}));

// Messages are {id, fn, args}; the reply is {id, value} or {id, error}.
self.onmessage = async ({data: {id, fn, args}}) => {
  await ready;
  const r = ifgrad[fn](...args);
  if (r.error !== undefined) { postMessage({id, error: r.error}); return; }
  postMessage({id, value: r.value}, buffers(r.value));
};

// buffers collects the byte arrays in v so they are moved, not copied.
function buffers(v, out = []) {
  if (v instanceof Uint8Array) out.push(v.buffer);
  else if (v && typeof v === 'object') for (const x of Object.values(v)) buffers(x, out);
  return out;
}
