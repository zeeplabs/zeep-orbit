# Built-in skills: provenance and attributions

Every built-in skill under this directory is authored or adapted for the Zeep
Orbit AI Builder (React + Vite + TypeScript + plain CSS + `HashRouter`, with
the builder's own tools). Each `SKILL.md` records its provenance in the
`source` frontmatter field. Skills that say "Written for the Orbit builder" are
original work of this project.

Two skills adapt third-party material. Both were rewritten for generated React
code and for the builder's tool set; the changes are listed below.

## orbit-frontend-design

Adapted from the `frontend-design` skill in the `anthropics/skills`
repository, licensed under the Apache License, Version 2.0. The full license
text is in `LICENSE-Apache-2.0.txt` next to this file (the copy that ships with
the upstream skill).

Changes made to the original: shortened and restructured; added a token plan
expressed as CSS variables in the template's `src/index.css`; added the rule
that the company design system and the template guide take precedence; removed
references to screenshots, external fonts and hosts, and to other products;
added motion, copy and quality-floor sections specific to this builder.

## orbit-accessibility

Adapted from the `fixing-accessibility` skill in the `ibelick/ui-skills`
repository, licensed under the MIT License:

> MIT License
>
> Copyright (c) 2026 Julien Thibeaut
>
> Permission is hereby granted, free of charge, to any person obtaining a copy
> of this software and associated documentation files (the "Software"), to deal
> in the Software without restriction, including without limitation the rights
> to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
> copies of the Software, and to permit persons to whom the Software is
> furnished to do so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all
> copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
> IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
> FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
> AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
> LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
> OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
> SOFTWARE.

Changes made to the original: rewritten as rules for code the agent generates
(removed the slash-command usage and the report-only review mode), added React
and `<dialog>` examples, and aligned the priorities with the builder's forms
and states.

## Adding or changing a built-in

Before merging a built-in that adapts third-party material, list it here with
the upstream source, its license (verified from the upstream repository, not
from memory) and what was changed. Anything without a clear permissive license
is written from scratch instead. After editing a built-in, bump its `version`
and run `go test ./internal/agent/skills -run TestBuiltinLint -update-lock`.
