# Changelog

## 1.0.0 (2026-11-23)


### Features

* **backend:** add Go Wails backend, SQLite store, LiteLLM client, and LangChainGo GPU agent ([fb76d9c](https://github.com/thenickfer/simple_chatbot/commit/fb76d9c5af031c7d6e9e79f151c4628e68950e71))
* **backend:** upgrade desktop runtime to Wails v3 (GTK4 + WebKitGTK 6.0) ([29c433f](https://github.com/thenickfer/simple_chatbot/commit/29c433fb3436edda138a5fa41a03baefdde68a05))
* **deploy:** add automated ollama-pull init service and dynamic LITELLM_OLLAMA_MODEL injection ([219456a](https://github.com/thenickfer/simple_chatbot/commit/219456acbfaa73e80e9c55a078fbbfea89b51927))
* **deploy:** add docker-compose, litellm config, CI release workflow, decisions log, and root README ([0f8abdc](https://github.com/thenickfer/simple_chatbot/commit/0f8abdc9ecd6a8dd447934823786a23b749b8e0a))
* **deploy:** document and support Google Gemini Free Tier and multi-model configuration ([d63d77d](https://github.com/thenickfer/simple_chatbot/commit/d63d77dbe2674421ccc2348619abc095b17209e5))
* **deploy:** env-driven LiteLLM upstream with OpenCode Zen option ([2d98c20](https://github.com/thenickfer/simple_chatbot/commit/2d98c201900e381df676bfa66629b37024abe666))
* **frontend:** implement Cobalt Lab v2 GPU Assistant single-page UI ([1ac78ad](https://github.com/thenickfer/simple_chatbot/commit/1ac78adfdb88772948e67c425e240c704ac24648))


### Bug Fixes

* **build:** resolve Wails binary path and add automatic standalone server fallback for dev and build ([66dd988](https://github.com/thenickfer/simple_chatbot/commit/66dd988bb2ce867840137ffbeb9c3903e01118fa))
* **deploy:** configure automatic LiteLLM fallback to local Ollama and 600s timeout limits ([d0444e0](https://github.com/thenickfer/simple_chatbot/commit/d0444e0b0213a2fc2519d0056883be5d6bbcc5df))
* **docker-compose:** docker dns ([7e3d594](https://github.com/thenickfer/simple_chatbot/commit/7e3d5944e4df14028d952be2ca2abae5df877928))
* **litellm:** use os.environ/LITELLM_MASTER_KEY in config.yaml and auto-load .env in Go app ([1e49fd7](https://github.com/thenickfer/simple_chatbot/commit/1e49fd76dd3f7e0b53f687a7b10add6c6d1be0de))
* **llm:** increase default LiteLLM/Ollama request timeout to 600s ([0e06206](https://github.com/thenickfer/simple_chatbot/commit/0e0620658050da1002e1b960b86d7777746a1b96))
* **llm:** probe /health/liveliness first with per-endpoint timeout in CheckHealth ([4955c31](https://github.com/thenickfer/simple_chatbot/commit/4955c312a8c32760796bdacc57bc4ac694bb4092))
