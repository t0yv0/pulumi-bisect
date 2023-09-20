# pulumi-bisect

Find the first failing Pulumi release.

Write a script `bad.sh` that returns exit code 1 if something is not right. Then run:

```
pulumi-bisect --from v3.0.0 --to v3.74.0 --cmd ./bad.sh
```

Note that `./bad.sh` will be called with a given version of `pulumi` in `PATH` and keep beeing called until the minimal version that introduced the bad behavior is located.
