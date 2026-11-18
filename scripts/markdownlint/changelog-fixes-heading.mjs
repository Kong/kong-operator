// Custom markdownlint rule: bugfix sections in changelogs must be titled
// "### Fixes", not "### Fixed".
//
// The rule only applies to files named CHANGELOG.md (in any directory); all
// other markdown files are skipped.
//
// Releases that were published with "### Fixed" before this rule existed can
// be grandfathered via the `allowed_releases` option, e.g.:
//
//   config:
//     changelog-fixes-heading:
//       allowed_releases:
//       - v2.3.3
//
// A release is identified by the text of the closest preceding level 2
// heading, with surrounding brackets stripped (`## [v2.3.3]` -> `v2.3.3`).

import path from "node:path";

/** @type {import("markdownlint").Rule} */
export default {
  names: ["KO001", "changelog-fixes-heading"],
  description: 'Bugfix sections must use the "### Fixes" heading',
  tags: ["headings", "changelog"],
  parser: "micromark",
  function: (params, onError) => {
    if (path.basename(params.name) !== "CHANGELOG.md") {
      return;
    }

    const allowedReleases = new Set(params.config?.allowed_releases ?? []);
    let currentRelease = "";

    for (const token of params.parsers.micromark.tokens) {
      if (token.type !== "atxHeading") {
        continue;
      }
      const sequence = token.children.find((t) => t.type === "atxHeadingSequence");
      const text = token.children.find((t) => t.type === "atxHeadingText");
      if (!sequence || !text) {
        continue;
      }
      const level = sequence.text.length;
      const title = text.text.trim();

      if (level === 2) {
        currentRelease = title.replace(/^\[/, "").replace(/\]$/, "");
        continue;
      }
      if (level !== 3 || title !== "Fixed" || allowedReleases.has(currentRelease)) {
        continue;
      }

      onError({
        lineNumber: token.startLine,
        detail: 'Use "### Fixes" instead of "### Fixed" for bugfix sections',
        context: token.text,
        fixInfo: {
          editColumn: text.startColumn,
          deleteCount: text.text.length,
          insertText: "Fixes",
        },
      });
    }
  },
};
