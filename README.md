<img src="build/modhound.png" height="36" align="right" alt="modhound">

# modhound

Finds and installs mod updates for your Minecraft modpack. Made for my own modpack, which I play on legacy TL from time to time, but works for updating mods in any modpack, as long as it has a folder with mods in it. 

modhound identifies every jar in the `mods` folder by its hash on CurseForge and Modrinth, GT New Horizons mods by the GTNH asset manifest, and a few mods by their GitHub releases. It only installs files built for the modpack's Minecraft version and loader, checks each download before replacing the old jar file, keeps the previous version of every mod it updates, and writes a report of what was updated. It also reads the class files of every mod to warn about mods that won't work together, and explains the game's latest crash. 

![modhound update list](screenshots/modhound-updates.png)

Currently supports Forge.

## Usage

1. Download `modhound.exe` from the [latest release](https://github.com/blackkriger/modhound/releases/latest) and run it.
2. modhound opens `%APPDATA%\.minecraft` if it has mods. For another modpack, click **Select folder** at the top and pick its minecraft folder, the one that contains `mods`. 
3. The modpack is checked right away. The list shows:
   - **Need you**: an update has to be downloaded by hand, or the mod has a problem that will likely break the game.
   - **Updates**: a newer file is available.
   - **Updated**: updated by modhound, the previous version can be restored.
   - **Not found**: the jar is not on GTNH, CurseForge or Modrinth.
   - **Removed**: removed by modhound, can be put back until modhound is closed.
   - **Skipped**: mods you chose not to update.

   The counters on the right filter the list.
4. Click **Update all**, or uncheck mods first to update only the selected ones. Hover a row to update or skip a single mod. Click a row for details and what's new.
5. When it finishes, the report is saved and the updated mods are listed on the right.
6. If you need a specific version of a mod, click a row and then **Select version**: every version built for your Minecraft version is listed, a click on a version shows its changes, and **Install** puts the chosen one in place of the current jar, newer or older. 

## Undo

modhound keeps the previous version of each mod it updates. Updating a mod again replaces the kept version with the one just replaced. Hover an updated mod and click **Undo**, or check several updated mods and click **Undo**, or use **Undo all** after an update. 

## Compatibility

modhound reads the class files of every mod and marks problems in the list and under **Take notice** in a mod's details:

- A mod uses a class, method or field that another mod in the modpack doesn't have. Red when an update caused it, grey for optional integrations that may not work.
- A mod needs JvmDowngrader classes that the modpack doesn't provide.
- A mod requires a mod that is not in the modpack. When modhound can find the missing mod on GTNH, Modrinth or CurseForge, the details offer **Install** for it, or **Restore** when it was removed with modhound.
- Two jar files are the same mod. Forge will not start with both. 

## Crashes

When the game has crashed, the right panel shows which mod crashed it and what was missing, with **Undo** for the update that caused it, also you can **Open report** from here. modhound looks at the newest file in `crash-reports` on start, after every check and change. A crash is no longer shown once it is dismissed or once the mods have changed in a way that fixes it. 

## Removing mods

Click a mod and use **Remove from modpack**. The details list the mods that require it and the mods that use it. Libraries that only the removed mod needed can be removed with it. Removed mods are listed under **Removed** with option for **Undo**, and are deleted for good when modhound is closed. 

## Server

Set **Server** in the settings to keep a server's mods in step with the modpack. Every mod updated in the modpack is then also updated in the server's mods folder, if the server has it. When the server is behind the modpack, the right panel shows how many mods differ and an option to **Sync** them. Server files are backed up and restored by **Undo** together with the modpack. A mod removed from the modpack is removed from the server too. 

With a server set, a mod's details have **Side**: Client, Server or Both. modhound fills it from Modrinth where it can. Mods marked Server or Both that the server doesn't have are copied to it on sync. 

**Server** takes either a folder on this computer or a remote server over SFTP:

- `D:\server`: a local server folder. 
- `user@host:/path/to/server` or `sftp://user@host:port/path/to/server`: a remote server. The sign-in uses the key in **SSH key**, by default the first of `id_ed25519`, `id_ecdsa` and `id_rsa` in `%USERPROFILE%\.ssh`. The host must already be in `%USERPROFILE%\.ssh\known_hosts`, so connect to it once with `ssh` first. Keys with a passphrase and PuTTY `.ppk` keys are not supported; convert a `.ppk` key to OpenSSH format with PuTTYgen. Replaced server files are kept in `modhound-backups` next to the server's `mods` folder. 

A local server has to be stopped before updating, because the files of a running server can't be replaced. A remote server can keep running while it is updated, but it has to be restarted afterwards to load the new mods. 

## CurseForge API key

CurseForge lookups need a CurseForge Core API key from https://console.curseforge.com. Enter it in the settings. Without a key, mods hosted only on CurseForge cannot be checked, and the few GTNH mods distributed through CurseForge cannot be downloaded. 

## Settings

Open the settings with the gear at the top. Changes are saved as you make them.

- **CurseForge API key**
- **Server**: a local server folder or a remote server.
- **SSH key**: the key for a remote server.
- **Theme**: System, Light or Dark.
- **Debug mode**: shows the log console and writes it to a file. 

## Files

- `%APPDATA%\modhound`: settings.
- `%LOCALAPPDATA%\modhound`: backups, reports, logs, the cached GTNH catalog and the class index of every mod.

## Updates

modhound checks for a new release on start. When one is available, **Update to X** appears in the settings under the version.

## Building

Requires Go and Node.js, and the [Wails](https://wails.io) CLI.

```
pwsh ./build.ps1
```

This builds `build/bin/modhound.exe` with the version from `build.ps1` and writes its SHA-256 also. The app icons and `build/modhound.png` are generated with `pwsh ./tools/genicons.ps1`.
