BOOK := ../book/src
SHELL := /bin/bash

# Detect sed variant (macOS uses gsed from Homebrew)
SED := $(shell command -v gsed 2>/dev/null || echo sed)

.PHONY: all man clean

# Default: assemble prompt and call API if key is set
all: src/prompt
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
	  echo "ANTHROPIC_API_KEY is not set."; \
	  echo "Submit src/prompt to Claude manually, save the result as src/super.1.md,"; \
	  echo "then run: make man"; \
	else \
	  $(MAKE) src/super.1.md && $(MAKE) man; \
	fi

# Step 1: assemble the prompt
src/prompt: src/base-prompt \
            src/extract-names.py \
            $(BOOK)/command/super.md \
            $(BOOK)/command/options.md \
            $(BOOK)/command/input.md \
            $(BOOK)/command/output.md \
            $(BOOK)/command/formats.md \
            $(BOOK)/getting-started/tldr.md \
            $(BOOK)/command/db.md \
            $(BOOK)/command/compile.md \
            $(BOOK)/command/dev.md
	@echo "Assembling prompt..."
	@cp src/base-prompt src/prompt
	@echo -e "\nThe \"super -h\" output:\n==============================" >> src/prompt
	@super -h | $(SED) 's/\x1b\[[0-9;]*m//g' >> src/prompt
	@for file in super.md options.md input.md output.md formats.md; do \
	  echo -e "\nContents of \"$$file\" from detailed mdbook documentation:" >> src/prompt; \
	  echo "================================================================" >> src/prompt; \
	  cat $(BOOK)/command/$$file >> src/prompt; \
	done
	@echo -e "\nSource material from detailed mdbook documentation for EXAMPLES in manpage:" >> src/prompt
	@echo "================================================================" >> src/prompt
	@cat $(BOOK)/getting-started/tldr.md >> src/prompt
	@for file in db.md compile.md dev.md; do \
	  echo -e "\nFirst 50 lines of \"$$file\" from detailed mdbook documentation:" >> src/prompt; \
	  echo "================================================================" >> src/prompt; \
	  head -50 $(BOOK)/command/$$file >> src/prompt; \
	done
	@echo -e "\nOperator, aggregate function, and function names that should appear bold in man page text:" >> src/prompt
	@echo "================================================================" >> src/prompt
	@python3 src/extract-names.py >> src/prompt
	@$(SED) -i "s/DATE_PLACEHOLDER/$$(date '+%B %Y')/" src/prompt
	@echo "Done. src/prompt is ready."

# Step 2: call Claude API to generate src/super.1.md
src/super.1.md: src/prompt
	@echo "Calling Claude API..."
	@jq -n --rawfile content src/prompt '{ \
	  model: "claude-sonnet-4-6", \
	  max_tokens: 4096, \
	  temperature: 0, \
	  messages: [{ role: "user", content: $$content }] \
	}' | curl -s https://api.anthropic.com/v1/messages \
	  --header "x-api-key: $$ANTHROPIC_API_KEY" \
	  --header "anthropic-version: 2023-06-01" \
	  --header "content-type: application/json" \
	  --data @- \
	  | jq -r '.content[0].text' > src/super.1.md
	@echo "Done. src/super.1.md is ready."

# Step 3: run pandoc
man: man1/super.1

man1/super.1: src/super.1.md
	@echo "Generating super.1..."
	@pandoc src/super.1.md -s -t man -o man1/super.1
	@echo "Done. man1/super.1 is ready."

clean:
	@rm -f src/prompt src/super.1.md man1/super.1
	@git checkout src/super.1.md man1/super.1 2>/dev/null || true
	@echo "Cleaned."
