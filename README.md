identify which of your colleagues are Going Through It. track their sleep schedule by ~~spying~~ checking-in on the timestamps of their git commits ^^

can pass e.g. `https://github.com/user git.my.site/user/repo codeberg.org/user` (sources can be account names or repos, either is fine)

0. `subjects.toml`

toml's a pretty good format for this. under each name, a list of URLs to source from

1. crawl github api for public repo names

this gets rate-limited to i believe 60 or 100 repos. more than enough data assuming recency.

TODO gitea/gitlab APIs

TODO ensure recency?

2. clone every repo without downloading blobs

go-git added [suppport](github.com/go-git/go-git/v5@96332667b1d7ee3a75c94b15379dc4a35d6dd2a5) for `--filter=blob:none`! time to not write my own structs for everything. this partial-clones git repos without downloading any of the files, just commit metadata; exactly what we need

3. nest iterate all repos for all commits, flatten timestamps into single array

pretty straightforward except for verifying authorship, especially for forked repos. not too hacky

4. profile someone's sleep schedule

this one's pretty easy even with weird sleep schedules. save a snapshot of their sleep distribution in 24 hour-buckets

5. optionally graph scatterplot or histo for a literal snapshot

6. repeat and look for changes

i envision this as a cronjob or a container

