# **Teletext64U** users manual

## Config utility
With this program you configure:
- IP-address and port of the machine where PetsciiProxy is running.
- Auto refresh time. A teletext page will automatically be downloaded again after the set seconds.
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
|←| Go to previous teletext page (max. 20 steps back). Resets when changing stations.|
|UP | Subpage up |
|DN | Subpage down |
|R | Refresh current teletext page |
|B | Bold font |
|T | Thin font |
|S | Switch station (carousel) |
|M | Switch station (menu list) |
|C | Shows/hides concealed text |
|F1 | Fastext red |
|F3 | Fastext green |
|F5 | Fastext yellow |
|F7 | Fastext cyan |

## PetsciiProxy 
This runs on your PC (or Mac/Linux/NAS/..) and acts as a bridge between the internet and your C64 Ultimate/Other Ultimate product running Teletext64U. The default listening port is 8080. You can change the port by starting PetsciiProxy with a command line parameter. Start the program with --help for all parameter options.

