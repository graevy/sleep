track git committer sleep schedules ^^

#### 0. `subjects.toml`

looks like

```
[graevy]
github.com/graevy
git.devhack.net/a/member-prod

[someoneelse]
github.com/them/project
https://codeberg.org/them
```

#### 1. crawl github/gitlab/gitea api for public repo names

this gets rate-limited to i believe 60 or 100 repos. more than enough data assuming recency.

#### 2. clone repos without downloading blobs

first, check API to make sure the repo was last updated within our obseravtion window (default 3 months)

go-git added [support](github.com/go-git/go-git/v5@96332667b1d7ee3a75c94b15379dc4a35d6dd2a5) for `--filter=blob:none`! time to not write my own structs for everything. this partial-clones git repos without downloading any of the files, just commit metadata; exactly what we need

#### 3. nest iterate all repos for all commits, flatten timestamps into single array

pretty straightforward except for verifying authorship, especially for forked repos. not too hacky

#### 4. profile someone's sleep schedule

this one's pretty easy even with weird sleep schedules. save a snapshot of their sleep distribution in 24 hour-buckets

TODO: circular kernel density estimation probably best way to parse drifts in sleep schedule over time

#### 5. optionally graph scatterplot or histo

#### 6. repeat and look for changes

i envision this as a cronjob or a container


### Flags

`-s, --since`
    number of days of commit history to observe. defaults to 90

`-w, --write`
    whether to write a snapshot. defaults to true

`-o, --stdout`
    whether to print a simple sleep histogram. defaults to true

`-p, --plot-scatter`
    whether to graph a scatterplot png. defaults to false

`-h, --plot-histo`
    whether to graph a histogram png. defaults to false

`-u, --user`
    expects a user:sources mapping e.g. `someone@github.com/someone,https://forgejo.their.site/their/project`. when supplied, does not parse `subjects.toml`

