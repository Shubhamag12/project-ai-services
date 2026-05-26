- Access the Catalog UI at https://{{ .CATALOG_UI_DOMAIN }}{{ if and (ne .HTTPS_PORT "") (ne .HTTPS_PORT "443") }}:{{ .HTTPS_PORT }}{{ end }}

- Access the Catalog Backend at https://{{ .CATALOG_API_DOMAIN }}{{ if and (ne .HTTPS_PORT "") (ne .HTTPS_PORT "443") }}:{{ .HTTPS_PORT }}{{ end }}
