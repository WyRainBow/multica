package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueReceiptCmd = &cobra.Command{
	Use:   "receipt <issue-id>",
	Short: "Record or show the delivery-verification receipt that gates done (COC-282)",
	Long: "Done on a card that declares a delivery (git.* metadata) requires a receipt: a " +
		"verified statement of where the code ended up. With --result, records a receipt " +
		"(merged / delivered_without_mr / abandoned / unknown; unknown needs --reason). " +
		"The fingerprint binds it to the current declarations — change any git.* key and " +
		"the receipt goes stale, done will ask for a re-verify. Without --result, shows the " +
		"latest receipt and whether it is still valid.\n\n" +
		"--verify-local <repo-path> attaches machine-checked evidence before recording: " +
		"40-char SHA object existence plus git merge-base --is-ancestor of the delivery " +
		"SHA under --target (default origin/HEAD), the same checks agent-progress's " +
		"verify.sh runs. Facts machine-checked, semantics human-decided.",
	Args: exactArgs(1),
	RunE: runIssueReceipt,
}

func runIssueReceipt(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	path := "/api/issues/" + issueRef.ID + "/delivery-receipt"

	result, _ := cmd.Flags().GetString("result")
	if result == "" {
		var receipt map[string]any
		if err := client.GetJSON(ctx, path, &receipt); err != nil {
			return fmt.Errorf("show receipt: %w", err)
		}
		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			return cli.PrintJSON(os.Stdout, receipt)
		}
		valid := "stale (declarations changed)"
		if b, _ := receipt["valid"].(bool); b {
			valid = "valid"
		}
		fmt.Fprintf(os.Stdout, "Receipt %s · %s · %s\n  delivery: %s\n  created: %s\n",
			strVal(receipt, "id"), strVal(receipt, "result"), valid,
			strVal(receipt, "delivery_ref"), strVal(receipt, "created_at"))
		if r := strVal(receipt, "reason"); r != "" {
			fmt.Fprintf(os.Stdout, "  reason: %s\n", r)
		}
		return nil
	}

	body := map[string]any{"result": result}
	if v, _ := cmd.Flags().GetString("reason"); v != "" {
		body["reason"] = v
	}
	if repo, _ := cmd.Flags().GetString("verify-local"); repo != "" {
		evidence, err := verifyAncestryLocal(ctx, client, issueRef.ID, repo, cmd)
		if err != nil {
			return err
		}
		body["evidence"] = evidence
	}

	var receipt map[string]any
	if err := client.PostJSON(ctx, path, body, &receipt); err != nil {
		return fmt.Errorf("record receipt: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, receipt)
	}
	fmt.Fprintf(os.Stdout, "Receipt recorded: %s (delivery: %s)\nIssue can now transition to done.\n",
		strVal(receipt, "result"), strVal(receipt, "delivery_ref"))
	return nil
}

// verifyAncestryLocal runs agent-progress-style machine checks against a local
// clone: the delivery SHA exists as an object and is an ancestor of the
// target ref. Output is attached verbatim as receipt evidence.
func verifyAncestryLocal(ctx context.Context, client *cli.APIClient, issueID, repo string, cmd *cobra.Command) (string, error) {
	deliverySHA, target, err := deliveryTargets(ctx, client, issueID, cmd)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "verify-local @ %s\n", repo)

	if len(deliverySHA) >= 7 {
		out, err := runGitLocal(repo, "cat-file", "-e", deliverySHA+"^{commit}")
		fmt.Fprintf(&sb, "$ git cat-file -e %s^{commit}\n%s(exit %d)\n", deliverySHA, out, exitCode(err))
		if err != nil {
			return sb.String(), fmt.Errorf("delivery SHA %s does not exist as a commit object in %s", deliverySHA, repo)
		}
	}

	out, err := runGitLocal(repo, "merge-base", "--is-ancestor", deliverySHA, target)
	fmt.Fprintf(&sb, "$ git merge-base --is-ancestor %s %s\n%s(exit %d)\n", deliverySHA, target, out, exitCode(err))
	if err != nil {
		fmt.Fprintf(&sb, "NOT an ancestor — code not merged under %s. Use result=delivered_without_mr or abandoned if that is the truth.\n", target)
	} else {
		fmt.Fprintf(&sb, "ancestry OK: %s is contained in %s\n", deliverySHA, target)
	}
	return sb.String(), nil
}

func deliveryTargets(ctx context.Context, client *cli.APIClient, issueID string, cmd *cobra.Command) (sha, target string, err error) {
	var issue map[string]any
	if err = client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID), &issue); err != nil {
		return "", "", fmt.Errorf("load issue: %w", err)
	}
	meta, _ := issue["metadata"].(map[string]any)
	sha = metaStr(meta, "git.delivery_sha")
	if sha == "" {
		return "", "", fmt.Errorf("issue metadata has no git.delivery_sha — set it (full 40-char SHA) before --verify-local")
	}
	target, _ = cmd.Flags().GetString("target")
	if target == "" {
		target = "origin/HEAD"
	}
	return sha, target, nil
}

func runGitLocal(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func metaStr(meta map[string]any, key string) string {
	v, _ := meta[key].(string)
	return strings.TrimSpace(v)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
