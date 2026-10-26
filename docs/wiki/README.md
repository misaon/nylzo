# Publishing the GitHub Wiki

The [public Wiki](https://github.com/misaon/nylzo/wiki) is the documentation entry point. Its Markdown sources stay in this repository for pull-request review. Pages in this directory supply the user guides; existing CLI, operations, release, governance, security, and contributor guides are included by the renderer rather than copied into a second source.

From a clean, reviewed checkout of `main`, generate the Wiki:

```sh
mise exec -- node tools/wiki.mts
```

The renderer writes `dist/wiki`, rewrites repository-relative links for the Wiki, validates linked local files and Wiki page names, and adds links back to reviewed source files. It does not contact GitHub or publish anything.

Clone `https://github.com/misaon/nylzo.wiki.git` outside the source checkout, fast-forward it to the current remote head, and copy the generated Markdown files into it. Inspect the diff, sign the documentation commit with the maintainer's registered signing key, and push normally. Never force-push or overwrite unrelated pages. Confirm the public Home page, sidebar, and changed pages after publication.

Keep Wiki editing restricted to repository collaborators. GitHub Wiki pushes do not go through the main repository's pull-request rules; use source PRs for substantive changes and republish after merge. Do not add a privileged Actions workflow or a long-lived publishing token solely to update the Wiki.
