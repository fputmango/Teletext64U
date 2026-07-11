# **Teletext64U** users manual

## Config utility
With this program you configure:
- If you want to use the public petsciiproxy.nl server (recommended) or a local server.
- IP-address and port of the machine where PetsciiProxy is running.
- Auto refresh time. A teletext page will automatically be downloaded again after the set seconds.
- Cycle time. If a page consists of subpages, it will auto rotate through the subpages after the default or set seconds.
- Favorite station and page. This teletext service and page to be shown at startup.

| Key | Description |
| :--- | :--- |
|UP | Navigate up with CRSR up key |
|DN | Navigate down with CRSR down key |
|RETURN | Edit / change the value of the selected item |
|F1 | Save config to disk |
|F3 | Exit the program |


## Teletext64U

| Key | Description |
| :--- | :--- |
|CLR HOME|Go to favorite start page |
|+| Go to current page +1 / next page|
|-| Go to current page -1 / previous page|
|↑| Step back through visited pages|
|←| Go back to the WiC64 Portal|
|UP | Subpage up |
|DN | Subpage down |
|R | Refresh current teletext page |
|Space | Stop/Continue auto rotating subpages |
|B | Bold font |
|T | Thin font |
|S | Switch station (carousel) |
|M | Switch station (menu list) - Navigatge: cursor up/down or type first letter for quick select; RETURN selects station |
|C | Shows/hides concealed text |
|F1 | Fastext red |
|F3 | Fastext green |
|F5 | Fastext yellow |
|F7 | Fastext cyan |
|F8 | Help screen |
|C= + S | Stopwatch ON/OFF toggle - measures total time needed for HTTP fetch, decode & rendering page on screen |

## PetsciiProxy 
Using the public petsciiproxy.nl server is highly recommended and is the default setting. But if you do want to run it locally you could. This runs on your PC (or Mac/Linux/NAS/..) and acts as a bridge between the internet and your C64 Ultimate/Other Ultimate product running Teletext64U. The default listening port is 8080. You can change the port by starting PetsciiProxy with a command line parameter. Start the program with --help for all parameter options.
