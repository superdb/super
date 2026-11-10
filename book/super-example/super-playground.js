import {Editor} from './editor';
import {engineLoaded, super_} from './super';

export class SuperPlayground {
  static setup(node, onQueryOrInputChange) {
    const playground = new SuperPlayground(node);
    node.__super_playground__ = playground;
    playground.setup(onQueryOrInputChange);
  }

  static teardown(node) {
    const playground = node.__super_playground__;
    if (playground) {
      playground.teardown();
      delete node.__super_playground__;
    }
  }

  constructor(node) {
    this.node = node;
  }

  setup(onQueryOrInputChange) {
    this.input = new Editor({
      node: this.node.querySelector('.input pre'),
      onChange: () => {
        onQueryOrInputChange?.call(null, this.query.value, this.input.value);
        this.run();
      }
    });
    this.query = new Editor({
      node: this.node.querySelector('.query pre'),
      onChange: () => {
        onQueryOrInputChange?.call(null, this.query.value, this.input.value);
        this.run();
      },
      language: 'sql'
    });
    this.result = new Editor({
      node: this.node.querySelector('.result pre')
    });
    onQueryOrInputChange?.call(null, this.query.value, this.input.value);
    this.run({initial: true});
    engineLoaded.then((err) => {
      if (err) this.showLoadNotice(err);
    });
  }

  // If the engine can't load, the example still shows the result from the
  // docs, but edits can't update it. Say so prominently, above the example.
  showLoadNotice(err) {
    if (this.notice) return;
    const site = document.location.hostname;
    this.notice = document.createElement('div');
    this.notice.className = 'super-example-notice';
    this.notice.setAttribute('role', 'alert');
    this.notice.textContent = err.status === 403
      ? `This example isn't interactive because your browser is blocking ` +
        `cookies for ${site}. The result shown is from the docs and won't ` +
        `change if you edit the query or input. To make examples ` +
        `interactive, allow cookies for ${site} and reload the page.`
      : `This example isn't interactive because the SuperDB engine failed ` +
        `to load (${err.message}). The result shown is from the docs and ` +
        `won't change if you edit the query or input. Try reloading the page.`;
    this.node.before(this.notice);
  }

  teardown() {
    this.notice?.remove();
    this.input.teardown();
    this.query.teardown();
    this.result.teardown();
  }

  async run({initial = false} = {}) {
    try {
      this.result.value = await super_({
        query: this.query.value,
        input: this.input.value
      });
    } catch (e) {
      // Engine didn't load. Keep the docs' result on first render, but don't
      // leave a stale result after an edit, where it would look like the
      // answer to the edited query.
      if (!initial) this.result.value = '(Not run: see the notice above this example.)';
    }
  }
}
