package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	managementv1alpha1 "github.com/caelis-labs/memory/api/memory/management/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
	localclient "github.com/caelis-labs/memory/sdk/go/memory/local"
	managementclient "github.com/caelis-labs/memory/sdk/go/memory/management"
)

type authorizationFile struct {
	Capability v1alpha1.CapabilityToken `json:"capability"`
	ActorRef   string                   `json:"actor_ref"`
	Audience   v1alpha1.Audience        `json:"audience"`
	ExpiresAt  time.Time                `json:"expires_at,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "memoryctl:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	global := flag.NewFlagSet("memoryctl", flag.ContinueOnError)
	var socketPath, credentialPath, issuerCredentialPath string
	global.StringVar(&socketPath, "socket", "", "memoryd Unix socket (required)")
	global.StringVar(&credentialPath, "management-credential", "", "management credential file")
	global.StringVar(&issuerCredentialPath, "issuer-credential", "", "issuer credential file")
	if err := global.Parse(arguments); err != nil {
		return err
	}
	if socketPath == "" || global.NArg() == 0 {
		return fmt.Errorf("usage: memoryctl -socket PATH [-management-credential FILE] [-issuer-credential FILE] COMMAND")
	}
	command := global.Arg(0)
	commandArgs := global.Args()[1:]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if command == "health" || command == "ready" || command == "compatibility" {
		client := localclient.NewClient(socketPath)
		if command == "health" {
			return client.Health(ctx)
		}
		if command == "ready" {
			return client.Ready(ctx)
		}
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		var serviceVersion, buildRevision string
		flags.StringVar(&serviceVersion, "service-version", "", "expected packaged service version")
		flags.StringVar(&buildRevision, "build-revision", "", "expected packaged source revision")
		if err := flags.Parse(commandArgs); err != nil {
			return err
		}
		if (serviceVersion == "") != (buildRevision == "") {
			return fmt.Errorf("compatibility requires both -service-version and -build-revision when either is set")
		}
		var response v1alpha1.CompatibilityResponse
		var err error
		if serviceVersion == "" {
			response, err = client.Compatibility(ctx)
		} else {
			response, err = client.CheckCompatibility(ctx, localclient.CompatibilityExpectation{
				ServiceVersion: serviceVersion, BuildRevision: buildRevision,
			})
		}
		return writeResult(response, err)
	}
	if command == "remember" || command == "recall" {
		return runDataPlane(ctx, socketPath, command, commandArgs)
	}
	if command == "issue" {
		var request v1alpha1.CapabilityIssueRequest
		outputPath, err := readSecretOutputFlags(command, commandArgs, "authorization-output", &request)
		if err != nil {
			return err
		}
		if issuerCredentialPath == "" {
			return fmt.Errorf("-issuer-credential is required for issue")
		}
		issuerCredential, err := readIssuerCredential(issuerCredentialPath, request.PrincipalRef)
		if err != nil {
			return err
		}
		output, err := reserveSecretOutput(outputPath)
		if err != nil {
			return err
		}
		response, err := localclient.NewIssuerClient(socketPath, issuerCredential).IssueCapability(ctx, request)
		if err != nil {
			_ = output.Close()
			_ = os.Remove(outputPath)
			return err
		}
		authorization := authorizationFile{
			Capability: response.Token,
			ActorRef:   request.ActorRef,
			Audience:   request.Audience,
			ExpiresAt:  response.ExpiresAt,
		}
		if err := writeSecretJSON(output, authorization); err != nil {
			return fmt.Errorf("capability issued but authorization output failed: %w", err)
		}
		return writeResult(map[string]any{"issued": true, "authorization_file": outputPath, "expires_at": response.ExpiresAt}, nil)
	}
	if credentialPath == "" {
		return fmt.Errorf("-management-credential is required for management command %q", command)
	}
	credential, err := readCredential(credentialPath)
	if err != nil {
		return err
	}
	admin := managementclient.NewClient(socketPath, credential)
	switch command {
	case "bootstrap":
		var request managementv1alpha1.BootstrapRequest
		outputPath, err := readSecretOutputFlags(command, commandArgs, "issuer-output", &request)
		if err != nil {
			return err
		}
		output, err := reserveSecretOutput(outputPath)
		if err != nil {
			return err
		}
		response, err := admin.Bootstrap(ctx, request)
		if err != nil {
			_ = output.Close()
			_ = os.Remove(outputPath)
			return err
		}
		if err := writeSecretJSON(output, response); err != nil {
			return fmt.Errorf("bootstrap committed but issuer credential output failed: %w", err)
		}
		return writeResult(map[string]any{"bootstrapped": true, "issuer_credentials_file": outputPath}, nil)
	case "inspect":
		response, err := admin.Inspect(ctx)
		return writeResult(response, err)
	case "search":
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		var query, spaceID string
		var limit int
		var includeCorrected bool
		flags.StringVar(&query, "query", "", "receipt text query")
		flags.StringVar(&spaceID, "space", "", "optional Space ID")
		flags.IntVar(&limit, "limit", 20, "maximum results (1..100)")
		flags.BoolVar(&includeCorrected, "include-corrected", false, "include shadowed original receipts")
		if err := flags.Parse(commandArgs); err != nil {
			return err
		}
		response, err := admin.SearchReceipts(ctx, managementv1alpha1.SearchReceiptsRequest{
			Query: query, SpaceID: v1alpha1.SpaceID(spaceID), Limit: limit, IncludeCorrected: includeCorrected,
		})
		return writeResult(response, err)
	case "trace-receipt":
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		var receiptID string
		flags.StringVar(&receiptID, "id", "", "Receipt ID from Recall evidence")
		if err := flags.Parse(commandArgs); err != nil {
			return err
		}
		response, err := admin.TraceReceipt(ctx, managementv1alpha1.TraceReceiptRequest{ReceiptID: v1alpha1.ReceiptID(receiptID)})
		return writeResult(response, err)
	case "correct-receipt":
		request, err := parseCorrectReceipt(command, commandArgs)
		if err != nil {
			return err
		}
		response, err := admin.CorrectReceipt(ctx, request)
		return writeResult(response, err)
	case "delete-receipt":
		request, err := parseDeleteReceipt(command, commandArgs)
		if err != nil {
			return err
		}
		response, err := admin.DeleteReceipt(ctx, request)
		return writeResult(response, err)
	case "rebuild-fts":
		if err := admin.RebuildFTS(ctx); err != nil {
			return err
		}
		return writeResult(map[string]bool{"rebuilt": true}, nil)
	case "revoke-grant":
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		var grantID string
		flags.StringVar(&grantID, "id", "", "Grant ID")
		if err := flags.Parse(commandArgs); err != nil {
			return err
		}
		if grantID == "" {
			return fmt.Errorf("revoke-grant requires -id")
		}
		if err := admin.RevokeGrant(ctx, v1alpha1.GrantID(grantID)); err != nil {
			return err
		}
		return writeResult(map[string]bool{"revoked": true}, nil)
	case "rotate-issuer":
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		var principalRef, outputPath string
		flags.StringVar(&principalRef, "principal", "", "issuer principal reference")
		flags.StringVar(&outputPath, "issuer-output", "", "new owner-only issuer output file")
		if err := flags.Parse(commandArgs); err != nil {
			return err
		}
		if principalRef == "" || outputPath == "" {
			return fmt.Errorf("rotate-issuer requires -principal and -issuer-output")
		}
		output, err := reserveSecretOutput(outputPath)
		if err != nil {
			return err
		}
		response, err := admin.RotateIssuerCredential(ctx, principalRef)
		if err != nil {
			_ = output.Close()
			_ = os.Remove(outputPath)
			return err
		}
		if err := writeSecretJSON(output, response); err != nil {
			return fmt.Errorf("issuer rotated but credential output failed; rotate again: %w", err)
		}
		return writeResult(map[string]any{"rotated": true, "issuer_credentials_file": outputPath}, nil)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runDataPlane(ctx context.Context, socketPath, command string, arguments []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	var authPath, value, key, token string
	flags.StringVar(&authPath, "authorization", "", "Runtime authorization JSON file")
	if command == "remember" {
		flags.StringVar(&value, "text", "", "fact text")
		flags.StringVar(&key, "idempotency-key", "", "stable effect key")
	} else {
		flags.StringVar(&value, "query", "", "Recall query")
		flags.StringVar(&token, "consistency-token", "", "optional causal cursor")
	}
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if authPath == "" || value == "" {
		return fmt.Errorf("%s requires -authorization and value", command)
	}
	var file authorizationFile
	if err := readJSONFile(authPath, &file); err != nil {
		return err
	}
	auth := v1alpha1.CallAuthorization{Capability: file.Capability, ActorRef: file.ActorRef, Audience: file.Audience}
	client := localclient.NewClient(socketPath)
	if command == "remember" {
		if key == "" {
			return fmt.Errorf("remember requires -idempotency-key")
		}
		response, err := client.Remember(ctx, auth, v1alpha1.RememberRequest{Text: value, IdempotencyKey: key})
		return writeResult(response, err)
	}
	response, err := client.Recall(ctx, auth, v1alpha1.RecallRequest{
		Query:               value,
		MinConsistencyToken: v1alpha1.ConsistencyToken(token),
		Budget:              v1alpha1.RecallBudget{MaxFragments: 8, MaxBytes: 4096, DeadlineMS: 5000},
	})
	return writeResult(response, err)
}

func parseCorrectReceipt(command string, arguments []string) (managementv1alpha1.CorrectReceiptRequest, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	var receiptID, text, reason, key string
	flags.StringVar(&receiptID, "id", "", "Receipt ID")
	flags.StringVar(&text, "text", "", "replacement fact text")
	flags.StringVar(&reason, "reason", "", "operator audit reason")
	flags.StringVar(&key, "idempotency-key", "", "stable management effect key")
	if err := flags.Parse(arguments); err != nil {
		return managementv1alpha1.CorrectReceiptRequest{}, err
	}
	request := managementv1alpha1.CorrectReceiptRequest{
		ReceiptID: v1alpha1.ReceiptID(receiptID), ReplacementText: text, Reason: reason, IdempotencyKey: key,
	}
	return request, request.Validate()
}

func parseDeleteReceipt(command string, arguments []string) (managementv1alpha1.DeleteReceiptRequest, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	var receiptID, reason, key string
	flags.StringVar(&receiptID, "id", "", "Receipt ID")
	flags.StringVar(&reason, "reason", "", "operator audit reason")
	flags.StringVar(&key, "idempotency-key", "", "stable management effect key")
	if err := flags.Parse(arguments); err != nil {
		return managementv1alpha1.DeleteReceiptRequest{}, err
	}
	request := managementv1alpha1.DeleteReceiptRequest{
		ReceiptID: v1alpha1.ReceiptID(receiptID), Reason: reason, IdempotencyKey: key,
	}
	return request, request.Validate()
}

func readSecretOutputFlags(command string, arguments []string, outputFlag string, output any) (string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	var path, outputPath string
	flags.StringVar(&path, "file", "", "request JSON file")
	flags.StringVar(&outputPath, outputFlag, "", "new owner-only secret output file")
	if err := flags.Parse(arguments); err != nil {
		return "", err
	}
	if path == "" || outputPath == "" {
		return "", fmt.Errorf("%s requires -file and -%s", command, outputFlag)
	}
	return outputPath, readJSONFile(path, output)
}

func readJSONFile(path string, output any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewDecoder(file).Decode(output); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func readCredential(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	credential := strings.TrimSpace(string(value))
	if credential == "" {
		return "", fmt.Errorf("credential is empty")
	}
	return credential, nil
}

func readIssuerCredential(path, principalRef string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var bootstrap managementv1alpha1.BootstrapResponse
	if json.Unmarshal(value, &bootstrap) == nil && bootstrap.IssuerCredentials[principalRef] != "" {
		return bootstrap.IssuerCredentials[principalRef], nil
	}
	var rotated managementv1alpha1.IssuerAuthorization
	if json.Unmarshal(value, &rotated) == nil && rotated.PrincipalRef == principalRef && rotated.Credential != "" {
		return rotated.Credential, nil
	}
	credential := strings.TrimSpace(string(value))
	if strings.HasPrefix(credential, "{") {
		return "", fmt.Errorf("issuer credential file does not contain principal %q", principalRef)
	}
	if credential == "" {
		return "", fmt.Errorf("issuer credential is empty")
	}
	return credential, nil
}

func reserveSecretOutput(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("reserve secret output %s: %w", path, err)
	}
	return file, nil
}

func writeSecretJSON(file *os.File, value any) error {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	writeErr := encoder.Encode(value)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func writeResult(value any, err error) error {
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
