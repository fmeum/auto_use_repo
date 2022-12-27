package update

import (
	"fmt"
	"strings"

	"github.com/bazelbuild/buildtools/build"
	"github.com/bazelbuild/buildtools/labels"
)

// moduleRepoNames returns a map from apparent repository names to module names.
func moduleRepoNames(f *build.File) map[string]string {
	repos := make(map[string]string)

	for _, module := range f.Rules("module") {
		if name := module.AttrString("name"); name != "" {
			repos[""] = name
			if repoName := module.AttrString("repo_name"); repoName != "" {
				repos[repoName] = name
			} else {
				repos[name] = name
			}
		}
	}

	for _, bazelDep := range f.Rules("bazel_dep") {
		if name := bazelDep.AttrString("name"); name != "" {
			if repoName := bazelDep.AttrString("repo_name"); repoName != "" {
				repos[repoName] = name
			} else {
				repos[name] = name
			}
		}
	}

	return repos
}

type extensionProxyInfo struct {
	Name      string
	LastUsage build.Expr
}

// proxyCreatingTag returns the name of the proxy used to create a tag, or an empty string if the
// call expression is not creating a tag.
func proxyCreatingTag(call *build.CallExpr) string {
	if _, ok := call.X.(*build.DotExpr); !ok {
		return ""
	}
	dot := call.X.(*build.DotExpr)
	if _, ok := dot.X.(*build.Ident); !ok {
		return ""
	}
	return dot.X.(*build.Ident).Name
}

// parseAsUseExtensionCall returns the proxy name, the bzl file and the extension name if the given
// stmt is a call to use_extension, or empty strings otherwise.
func parseAsUseExtensionCall(stmt build.Expr) (proxy, bzlFile, name string) {
	assignment := stmt.(*build.AssignExpr)
	if _, ok := assignment.LHS.(*build.Ident); !ok {
		return
	}
	if _, ok := assignment.RHS.(*build.CallExpr); !ok {
		return
	}
	call := assignment.RHS.(*build.CallExpr)
	if call.X.(*build.Ident).Name != "use_extension" {
		return
	}
	if len(call.List) < 2 {
		return
	}
	bzlFileExpr, ok := call.List[0].(*build.StringExpr)
	if !ok {
		return
	}
	nameExpr, ok := call.List[1].(*build.StringExpr)
	if !ok {
		return
	}
	return assignment.LHS.(*build.Ident).Name, bzlFileExpr.Value, nameExpr.Value
}

// extensionProxies returns a map from extension identifiers in the form
// `@module_name//path/to/extension.bzl%name` to information about all proxies created for that
// extension and their last usage (use_extension or tag).
func extensionProxies(f *build.File, repos map[string]string) map[string][]extensionProxyInfo {
	extensionToProxies := make(map[string][]extensionProxyInfo)
	proxies := make(map[string]*extensionProxyInfo)

	for _, stmt := range f.Stmt {
		if call, ok := stmt.(*build.CallExpr); ok {
			proxy, found := proxies[proxyCreatingTag(call)]
			if found {
				proxy.LastUsage = stmt
			}
			continue
		}
		proxy, bzlFile, name := parseAsUseExtensionCall(stmt)
		if proxy == "" {
			continue
		}

		bzlFileLabel := labels.Parse(bzlFile)
		moduleName, ok := repos[bzlFileLabel.Repository]
		if ok {
			bzlFileLabel.Repository = moduleName
		}
		extension := bzlFileLabel.Format() + "%" + name

		proxyInfo := extensionProxyInfo{
			Name:      proxy,
			LastUsage: stmt,
		}
		extensionToProxies[extension] = append(extensionToProxies[extension], proxyInfo)
		proxies[proxy] = &proxyInfo
	}

	return extensionToProxies
}

// parseAsUseRepoCall returns the proxy name and non-proxy argument list if the given stmt is a
// use_repo call or an empty string and slice otherwise.
func parseAsUseRepoCall(e build.Expr) (string, []build.Expr) {
	call, ok := e.(*build.CallExpr)
	if !ok {
		return "", nil
	}
	callee, ok := call.X.(*build.Ident)
	if !ok || callee.Name != "use_repo" {
		return "", nil
	}
	if len(call.List) < 1 {
		return "", nil
	}
	proxy, ok := call.List[0].(*build.Ident)
	if !ok {
		return "", nil
	}
	return proxy.Name, call.List[1:]
}

func useRepoCalls(f *build.File) map[string][]*build.CallExpr {
	calls := make(map[string][]*build.CallExpr)

	for _, stmt := range f.Stmt {
		proxy, _ := parseAsUseRepoCall(stmt)
		if proxy != "" {
			calls[proxy] = append(calls[proxy], stmt.(*build.CallExpr))
		}
	}

	return calls
}

func removeEmptyUseRepos(f *build.File) {
	insertAt := 0
	for i := 0; i < len(f.Stmt); i++ {
		stmt := f.Stmt[i]
		proxy, repos := parseAsUseRepoCall(stmt)
		if proxy == "" || len(repos) > 0 {
			// Only keep use_repo calls that declare at least one repo usage.
			f.Stmt[insertAt] = stmt
			insertAt++
		}
	}
	f.Stmt = f.Stmt[:insertAt]
}

// UpdateRepoUsages updates the use_repo calls in the given file to import the given repos per
// extension (ignoring existing keyword arguments to use_repo). An extension is identified via a key
// in the form `@module_name//path/to/extension.bzl%name`.
//
// If no use_repo call exists for a given extension, one is added after the last usage of the
// extensions proxy. If no use_extension call exists for the extension, an error is returned.
// Empty use_repo calls are removed.
func UpdateRepoUsages(f *build.File, usages map[string][]string) error {
	repos := moduleRepoNames(f)
	extensionToProxies := extensionProxies(f, repos)
	proxyToUseRepos := useRepoCalls(f)

	stmtPos := make(map[build.Expr]int)
	for i, stmt := range f.Stmt {
		stmtPos[stmt] = i
	}

	insertAfter := make(map[build.Expr]build.Expr)
	for extension, usedReposList := range usages {
		proxies, found := extensionToProxies[extension]
		if !found {
			if len(usedReposList) == 0 {
				continue
			} else {
				return fmt.Errorf("use_extension for %s not found in %s, but used repositories reported: %s", extension, f.Path, strings.Join(usedReposList, ", "))
			}
		}

		usedRepos := make(map[string]struct{})
		for _, repo := range usedReposList {
			usedRepos[repo] = struct{}{}
		}

		var lastUseRepo *build.CallExpr
		var lastUsage build.Expr
		// Remove all unnecessary use_repo arguments, but keep all (potentially empty) calls.
		for _, proxy := range proxies {
			pos := stmtPos[proxy.LastUsage]
			if pos > stmtPos[lastUsage] {
				lastUsage = proxy.LastUsage
			}
			for _, useRepo := range proxyToUseRepos[proxy.Name] {
				pos := stmtPos[useRepo]
				if pos > stmtPos[lastUseRepo] {
					lastUseRepo = useRepo
				}
				// Skip over the first argument, which is the extension proxy.
				insertAt := 1
				for i := 1; i < len(useRepo.List); i++ {
					// Anything other than a string literal is unexpected since managed use_repos
					// must not have keyword arguments.
					arg, ok := useRepo.List[i].(*build.StringExpr)
					if !ok {
						continue
					}
					if _, keep := usedRepos[arg.Value]; !keep {
						continue
					}
					useRepo.List[insertAt] = useRepo.List[i]
					insertAt++
					delete(usedRepos, arg.Value)
				}
				useRepo.List = useRepo.List[:insertAt]
			}
		}

		// Add all missing repositories to the last use_repo call and sort its list of arguments.
		// If there is no call yet, add one right after the last usage of the extension's proxies.
		var useRepo *build.CallExpr
		if lastUseRepo != nil {
			useRepo = lastUseRepo
		} else {
			useRepo = &build.CallExpr{}
			insertAfter[lastUsage] = useRepo
		}
		for repo := range usedRepos {
			useRepo.List = append(useRepo.List, &build.StringExpr{Value: repo})
		}
	}

	// Insert all new use_repo calls.
	var newStmts []build.Expr
	for _, stmt := range f.Stmt {
		newStmts = append(newStmts, stmt)
		if useRepo, found := insertAfter[stmt]; found {
			newStmts = append(newStmts, useRepo)
		}
	}
	f.Stmt = newStmts

	removeEmptyUseRepos(f)
	return nil
}
