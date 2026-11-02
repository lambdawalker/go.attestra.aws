package workflow

// sharedStackOutputs reads only the control-plane outputs consumed by the wizard.
// Other outputs, including the structured resourceInventory, are ignored by JSON decoding.
type sharedStackOutputs struct {
	APIURL            string `json:"apiUrl"`
	APIARN            string `json:"apiArn"`
	DomainTarget      string `json:"domainTarget"`
	IndexURL          string `json:"indexUrl"`
	RuntimeTableName  string `json:"runtimeTableName"`
	RuntimeTableARN   string `json:"runtimeTableArn"`
	AdminPoolID       string `json:"adminPoolId"`
	AdminLoginDomain  string `json:"adminLoginDomain"`
	AdminDomainTarget string `json:"adminDomainTarget"`
	AdminURL          string `json:"adminUrl"`
}
