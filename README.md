<img src="build/modhound.png" height="36" align="right" alt="modhound">

# modhound

Finds and installs mod updates for your minecraft modpack. Made for my own modpack, which I play on legacy TL from time to time, but works for updating mods in any TL modpack. 

modhound identifies every jar in the `mods` folder by its hash on CurseForge and Modrinth, and GT New Horizons mods by the GTNH asset manifest. It only installs files built for the modpack's Minecraft version and loader, checks each download before replacing the old jar, keeps the previous version of every mod it updates, and writes a report of what was updated. 

![modhound update list](screenshots/modhound-updates.png)

Currently supports Forge.

## Usage

1. Download `modhound.exe` from the [latest release](https://github.com/blackkriger/modhound/releases/latest) and run it.
2. modhound opens `%APPDATA%\.minecraft` if it has mods. For another modpack, click **Select folder** at the top and pick its minecraft folder, the one that contains `mods`. 
3. The modpack is checked right away. The list shows:
   - **Updates**: a newer file is available.
   - **Updated**: updated by modhound, the previous version can be restored.
   - **Need you**: an update exists but has to be downloaded by hand.
   - **Not found**: the jar is not on GTNH, CurseForge or Modrinth.
   - **Skipped**: mods you chose not to update.

   The counters on the right filter the list.
4. Click **Update all**, or uncheck mods first to update only the selected ones. Hover a row to update or skip a single mod. Click a row for details and what's new.
5. When it finishes, the report is saved and the updated mods are listed on the right. 

## Undo

modhound keeps the previous version of each mod it updates. Updating a mod again replaces the kept version with the one just replaced. Hover an updated mod and click **Undo**, or use **Undo all** after an update.

## Server

If a server folder is set in the settings, every mod updated in the modpack is also updated in the server's `mods` folder, if the server has it. When the server is behind the modpack, the right panel shows how many mods differ and a **Sync** link. Server files are backed up and restored by **Undo** together with the modpack.

Server files that are in use can't be replaced either, so the server sync fails while the server is running.

## CurseForge API key

CurseForge lookups need a CurseForge Core API key from https://console.curseforge.com. Enter it in the settings. Without a key, mods hosted only on CurseForge cannot be checked, and the few GTNH mods distributed through CurseForge cannot be downloaded. 

## Settings

Open the settings with the gear at the top. Changes are saved as you make them.

- **CurseForge API key**
- **Server folder**
- **Theme**: System, Light or Dark.
- **Debug mode**: shows the log console and writes it to a file. 

## Files

- `%APPDATA%\modhound`: settings.
- `%LOCALAPPDATA%\modhound`: backups, reports, logs and the cached GTNH catalog.

## Updates

modhound checks for a new release on start. When one is available, **Update to X** appears in the settings under the version.

## Building

Requires Go and Node.js, and the [Wails](https://wails.io) CLI.

```
pwsh ./build.ps1
```

This builds `build/bin/modhound.exe` with the version from `build.ps1` and writes its SHA-256 also. The app icons and `build/modhound.png` are generated with `pwsh ./tools/genicons.ps1`. 