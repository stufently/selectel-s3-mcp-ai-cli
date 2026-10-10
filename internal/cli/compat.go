package cli

import (
	"fmt"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"sort"
	"strings"
)

func Compat(p provider.Profile) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s S3 compatibility\n\nGenerated from the embedded provider profile.\n\n| Feature | Supported |\n|---|---|\n", p.DisplayName)
	names := make([]string, 0, len(p.Features))
	for name := range p.Features {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&out, "| %s | %t |\n", name, p.Features[name])
	}
	out.WriteString("\n| Pool | Endpoint | Location | Multi-AZ | Classes |\n|---|---|---|---|---|\n")
	for _, pool := range p.Pools {
		fmt.Fprintf(&out, "| %s | %s | %s | %t | %s |\n", pool.ID, pool.Endpoint, pool.Location, pool.MultiAZ, strings.Join(pool.StorageClasses, ", "))
	}
	out.WriteString("\n| Class | Name | RUB/GB/month (from) | Verified | Checked |\n|---|---|---|---|---|\n")
	for _, class := range p.StorageClasses {
		fmt.Fprintf(&out, "| %s | %s | %.2f | %t | %s |\n", class.ID, class.NameRU, class.Price, class.Verified, class.CheckedAt)
	}
	fmt.Fprintf(&out, "\nPresign maximum: %d seconds; verified: %t. %s\n", p.PresignMaxSeconds, p.Verified, p.PresignNote)
	return out.String()
}
