package mcp

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ChatGPTVaultPutHandler writes a relayed ChatGPT file into the vault using
// the existing authenticated vault service.
type ChatGPTVaultPutHandler func(context.Context, io.Reader, int64, string) (any, error)

// ChatGPTVaultPutInput is the typed argument shape for vault_put_file.
type ChatGPTVaultPutInput struct {
	File      ChatGPTFileInput `json:"file" jsonschema:"description=OpenAI file object with a temporary download URL."`
	VaultPath string           `json:"vault_path" jsonschema:"description=Vault destination path, e.g. vault:/docs/report.pdf."`
}

func ChatGPTVaultPutDescriptor(handler ChatGPTVaultPutHandler) ToolDescriptor {
	relayTimeout := 5 * time.Minute
	return ToolDescriptor{
		Name:        "vault_put_file",
		Title:       "Put a ChatGPT file in the Pinner vault",
		Description: "Store a user-selected ChatGPT file in the encrypted Pinner vault. Pinner fetches the temporary file URL locally and writes it through the existing authenticated vault service.",
		Category:    CategoryCore,
		InputSchema: toolSchemaFor[ChatGPTVaultPutInput](),
		Meta:        chatgptFileMeta(),
		Handler: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			in, err := decodeArgsFor[ChatGPTVaultPutInput]("ChatGPT vault", handler != nil, request)
			if err != nil {
				return ToolResult{}, err
			}
			if in.VaultPath == "" {
				return ToolResult{}, fmt.Errorf("vault_path is required")
			}
			_, body, size, err := openChatGPTInput(ctx, in.File, relayTimeout)
			if err != nil {
				return ToolResult{}, err
			}
			defer body.Close()

			writeCtx, cancel := context.WithTimeout(ctx, relayTimeout)
			defer cancel()
			result, err := handler(writeCtx, body, size, in.VaultPath)
			return wrapResult(result, err, "ChatGPT file stored in the vault.")
		},
	}
}
