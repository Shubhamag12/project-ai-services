- Add documents to your RAG application using the Digitize Documents UI at http://{{ .HOST_IP }}:{{ .DIGITIZE_UI_PORT }}.

- These documents are consumed by Q&A service.

- Run "ai-services application info {{ .AppName }} --runtime podman" to view service endpoints.
