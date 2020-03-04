Pugo
====

Pugo is a tool for performing various administrative tasks relating to
Imperial College Union's Club, Society and Project website hosting platform.
It is designed to be used by members of the ICU Sysadmin Team in their
day-to-day administration of the system. Tasks include:

* Synchronising access requests and revocations from eActivities to the
  icu-cdb configuration repository
* Making new sites
* Fixing file permissions on existing sites

## Installation and usage

### Installation

Pugo is written in [Go](https://golang.org/).  Install to your go workspace
as follows:

```
go get github.com/icunion/pugo/...
```

### Configuration

Pugo uses [Viper](https://github.com/spf13/viper) to manage configuration. A
configuration file is required to specify connection details for the
eActivities database, filesystem location of a checkout of the icu-cdb repo,
etc. The default location for the configuration file is `$HOME/.pugo.yaml`,
however this can be overridden with the `--config` flag. A sample
configuration file is included in the repo.

### Usage

Execute pugo with the relevant command. For example, to sync access
requests, run

```
pugo sync
```

The help command provides detailed usage information, e.g.

```
pugo help
pugo help sync
```

## Contact

[ICU Sysadmins](https://www.union.ic.ac.uk/sysadmin/)

Copyright (c) 2020 Imperial College Union

License: [MIT](https://opensource.org/licenses/MIT)
