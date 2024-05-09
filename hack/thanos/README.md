# Generating Thanos manifests using jsonnet

Thanos manifests in the config/thanos/manifests directory are generated using jsonnet.
This typically only needs to be done if updating the thanos version.
Generated manifests are checked in to source control.

To generate them, first you need [jsonnet](https://github.com/google/jsonnet#packages).

Then you can run `jb install && make thanos-manifests` from the root of the repo.
If there are changes to the files in the config/thanos/manifests directory, check them in to source control.

There is a jsonnetfile.json and jsonnetfile.lock.json file in the root of the repo.
These files contain jsonnet dependency information and can be managed by the `jb` cli.
More info on configuring the thanos dependency can found at https://github.com/thanos-io/kube-thanos/tree/main#installing

