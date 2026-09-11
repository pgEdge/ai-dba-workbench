# Enabling AI Mode

AI features require a configured LLM provider and a valid API key. When you
disable AI mode, the Workbench hides the AI analysis icon, Ask Ellie chat,
alert analysis, and chart analysis; see
[Running Without AI](ai-analysis.md#running-without-ai) for details. Follow
these steps to enable AI mode:

1. Choose a provider. The server supports `anthropic`, `openai`, `gemini`, and
   `ollama`.
2. Obtain an API key from your chosen provider, or install
   [Ollama](https://ollama.com) for a self-hosted local model. Ollama does not
   require an API key.
3. Store the API key in a file readable only by the server process. In the
   following example, the `echo` command writes the key and `chmod` restricts
   access to the file owner:

    ```bash
    echo "your-api-key" > ~/.anthropic-api-key
    chmod 600 ~/.anthropic-api-key
    ```

4. Update the `LLM CONFIGURATION` section of the server configuration file
   (`/etc/pgedge/ai-dba-server.yaml`), adding your LLM provider details and
   specifying the path to your provider's API key:

    ```yaml
    #=========================================================================
    # LLM CONFIGURATION (Web Client Chat Proxy)
    #=========================================================================
    #
    # The server proxies LLM requests for web clients and CLI tools that
    # don't have direct access to LLM APIs. API keys must be configured
    # for the chosen provider.
    #
    llm:
      # LLM provider: "anthropic", "openai", "gemini", or "ollama"
      # Default: anthropic
      # Default: claude-sonnet-4-5
      model: "claude-sonnet-4-5"

      #-----------------------------------------------------------------------
      # Anthropic Configuration
      #-----------------------------------------------------------------------

      # Anthropic API key — provide the path to a file containing the
      # key via anthropic_api_key_file (raw keys cannot be set inline
      # for security).
      # anthropic_api_key_file: "~/.anthropic-api-key"
    ```

5. Save the file and start the server; for example:

    ```bash
    /opt/ai-workbench/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml &
    ```
