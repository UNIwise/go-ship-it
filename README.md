# Go ship it!

![logo](docs/logo.png)

A github app for managing github releases

go-ship-it takes care of:

- pre-releasing
- changelog compilation
- release promotion
- version bumping

## Pre-releasing

Every time a commit is pushed to the default branch, go-ship-it will create a new release candidate

It will bump the version from the latest release, and append the next possible release candidate number

By default the patch is bumped

If any pull request included in the release has the `minor` or `major` label respectively, the minor or major label will be merged

If a pull request included in the release includes changelog on the form:

    ```release-note
    This text will show up in the release notes
    ```

## Promotion

Promotions can be triggered by editing a pre-release, and unchecking the pre-release checkbox. This will cause go-ship-it to

- Update the tag and name of the release to be the full release
- Remove all pre-releases of that release
- Create a new pre-release for the next version, if the targetBranch is not fully included in the full release

## Configuration

The behaviour can be configured with yaml in a `.ship-it` file at the root of the repository

| key          | default | description                                                                                    |
| ------------ | ------- | ---------------------------------------------------------------------------------------------- |
| targetBranch | ""      | Specifies which branch to trigger new releases from. Leave empty for default repository branch |
