module github.com/lambdawalker/go.attestra.aws/tools/deploy

go 1.27.3

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aws/aws-lambda-go v1.55.1 // indirect
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.21.20 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.73.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.46.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.13.11 // indirect
	github.com/aws/smithy-go v1.28.8 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/catppuccin/go v0.3.0 // indirect
	github.com/charmbracelet/bubbles v0.21.1-0.20250623103423-23b8fd6302d7 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/x/ansi v0.10.1 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13 // indirect
	github.com/charmbracelet/x/exp/strings v0.0.0-20240722160745-212f7b056ed0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/lambdawalker/go.apexfission.aws.verifyemail v0.0.0-20261103183508-9f175d1d1aa4 // indirect
	github.com/lambdawalker/go.appexfission.aws.card-capture v0.0.0-20261107054056-55e8f4033afe // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
)

replace github.com/lambdawalker/go.attestra.aws => ../..

require (
	github.com/aws/aws-sdk-go-v2 v1.47.8
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/huh v0.8.0
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/charmbracelet/x/term v0.2.1
	github.com/lambdawalker/go.apexfission.aws.auth v0.0.0-20261107054140-3c536bf9cbd8
	github.com/lambdawalker/go.attestra.aws v0.0.0-20261101213259-8f0f2848ad3d
	github.com/lambdawalker/go.infra.composer v0.0.0-20261103235125-ad4cfd268616
	golang.org/x/crypto v0.38.0
	golang.org/x/sys v0.36.0
	golang.org/x/text v0.25.0
)
