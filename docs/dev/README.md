# Developer documentation

This folder is for people who work **on** cppgo, not for people who use it. It
holds what the manual deliberately does not: how the project measures itself,
where each claim is tested, how the repository is laid out, how CI and releases
run, and why the design is what it is.

The user manual is [`README.md`](../../README.md) and the site under
[`site/`](../../site). The rules both follow are in
[`.agents/skills/extra/documentation-rules.md`](../../.agents/skills/extra/documentation-rules.md):
user documentation says what a feature does and is not reflective; the material
below lives here because it is about the project, its measurements or its
history.

| Document | Contents |
|---|---|
| [Benchmarks](benchmarks.md) | the measurement methodology and every number the manual quotes |
| [Evidence](evidence.md) | which test pins which claim, the fixtures, and the known defects |
| [Layout](layout.md) | the repository layout, the runtime/generator split, the generated files |
| [Process](process.md) | the project's own CI, the pinned image, coverage, site build, releases |
| [Decisions](decisions.md) | the history, the decision records, the alternatives, the status of designed work |
