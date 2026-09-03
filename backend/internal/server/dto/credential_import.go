package dto

// CompatibleAccountImportIssueResp 是兼容凭证解析、校验或导入阶段的问题。
type CompatibleAccountImportIssueResp struct {
	Stage   string `json:"stage"`
	File    string `json:"file,omitempty"`
	Index   *int   `json:"index,omitempty"`
	Name    string `json:"name,omitempty"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// CompatibleAccountImportResp 是对外兼容凭证导入 v1 契约。
type CompatibleAccountImportResp struct {
	ContractVersion int                                `json:"contract_version"`
	Platform        string                             `json:"platform"`
	Format          string                             `json:"format"`
	DryRun          bool                               `json:"dry_run"`
	Parsed          int                                `json:"parsed"`
	Imported        int                                `json:"imported"`
	Failed          int                                `json:"failed"`
	Issues          []CompatibleAccountImportIssueResp `json:"issues,omitempty"`
}

// CredentialAccountDeleteReq 仅允许通过账号主键 ID 删除账号。
type CredentialAccountDeleteReq struct {
	ID int `json:"id"`
}

// CredentialAccountBanReq 仅允许通过账号主键 ID 封禁账号。
type CredentialAccountBanReq struct {
	ID int `json:"id"`
}
