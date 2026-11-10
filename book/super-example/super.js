import './golang.js'

class Super {
  /**
   * Instantiates a new `Super` instance from a given wasm file URL.
   * @static
   * @param {string} url - The URL of the wasm file to instantiate.
   * @returns {Promise<Super>} A promise that resolves to a new `Super` instance.
   */
  static instantiate(url) {
    return fetch(url)
      .then((resp) => {
        if (!resp.ok) {
          const err = new Error(`HTTP ${resp.status} fetching ${url}`);
          err.status = resp.status;
          throw err;
        }
        return this.createInstance(resp);
      })
      .then((instance) => new Super(instance));
  }

  /**
   * @private
   */
  static async createInstance(response) {
    const go = new Go();
    const env = go.importObject;
    const wasm = await WebAssembly.instantiateStreaming(response, env);
    go.run(wasm.instance);
    return __go_wasm__;
  }

  /**
   * @private
   */
  constructor(instance) {
    this.instance = instance;
  }

  /**
   * Executes a query using the provided options and returns the result.
   * @async
   * @param {Object} opts - The options for the query.
   * @param {string} [opts.query] - The program to execute.
   * @param {string | ReadableStream} [opts.input] - The input data for the query.
   * @param {'auto' | 'arrows' | 'bsup' | 'csv' | 'json' | 'line' | 'parquet' | 'sup' | 'tsv' | 'zeek' | 'zjson'} [opts.inputFormat] - The format of the input data.
   * @param {'arrows' | 'bsup' | 'csv' | 'json' | 'line' | 'parquet' | 'sup' | 'tsv' | 'zeek' | 'zjson'} [opts.outputFormat] - The desired format of the output data.
   * @returns {Promise<any[]>} A promise that resolves to the processed query result.
   */
  run(args) {
    return this.instance.run({
      input: args.input,
      inputFormat: args.inputFormat,
      program: args.query,
      outputFormat: args.outputFormat,
    });
  }

  /**
   * Parses the given query string and returns the result.
   * @param {string} query - The query string to parse.
   * @returns {any} The parsed result.
   */
  parse(query) {
    return this.instance.parse(query);
  }
}

let db;

// Resolves once the engine has loaded (to null) or failed to load (to the
// Error), so callers can tell the two apart without polling.
export const engineLoaded = Super.instantiate(document.location.origin + '/super.wasm')
  .then((instance) => {
    db = instance;
    return null;
  })
  .catch((e) => {
    console.error('SuperDB engine (super.wasm) failed to load:', e);
    return e;
  });

export async function super_(...args) {
  const loadError = await engineLoaded;
  if (loadError) throw loadError;
  try {
    return await db.run(...args);
  } catch (e) {
    return e.toString();
  }
}
