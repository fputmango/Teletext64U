# PetsciiProxy TTI Page file format support

## Introduction to the TTI file format
The TTI file format is used by MRG Systems MiniTED and TED Scheduler to store teletext pages and associated data. The format maps control characters to high ASCII positions so that it can be stored in a text file format. It also stores metadata about the page such as page number and magazine.

Some software that can process TTI:

| Name | Description | Source |
|---|---|---|
| MiniTED / TED Scheduler | Original MRG broadcast software | https://zxnet.co.uk/teletext/documents/ttiformat.pdf |
| VBIT2 | Peter Kwan's teletext inserter (Raspberry Pi) | https://github.com/peterkvt80/vbit2 |
| wxTED | Page editor | – |
| QTeletextMaker | Level 2.5 page editor | https://github.com/gkthemac/QTeletextMaker |
| ttxweb | Fabian Schneider's Teletext to HTML generator | https://github.com/fabianswebworld/ttxweb |
| hacktv | Analogue TV transmitter (HackRF), sends TTI | https://manpages.ubuntu.com/manpages/noble/man1/hacktv.1.html |
| edit.tf | Browser page editor, TTI export | https://teletext.wiki.zxnet.co.uk/wiki/Edit.tf |
| ZXNet teletext editor | Browser page editor, TTI import | https://teletext.wiki.zxnet.co.uk/wiki/Zxnet_teletext_editor |
| TheMarco/teletext | Viewer and editor, TTI read/write | https://github.com/TheMarco/teletext |
| ttx42 | Terminal viewer, TTI read/write | https://github.com/bitplane/ttx42 |
| teletext-viewer | Web viewer for hosted TTI files | https://github.com/JamieNemeth |
| TtxFromTS | DVB recordings to TTI | https://github.com/orryverducci/TtxFromTS |
| CRYPTEXT | Live data service with TTI output | https://github.com/Mattbusel/cryptext |
| FAB Teletext | Professional editor (TTI unverified) | https://www.fab-online.com/teletext/api/ |


## How does it work?
Requesting a TTI file is pretty simple:

`http://petsciiproxy.nl/SERVICE/PPP/T`

or

`http://petsciiproxy.nl/SERVICE/PPP-NN/T`

where `SERVICE` is the name of the service (must be upper case, see table below), `PPP` a 3-digit page number, `NN` a 1 or 2 digit subpagenumber (optional) and `/T` is added to let the server know you want a TTI file. 

Examples: 

`http://petsciiproxy.nl/NOS-TT/705/T`

will get you all subpages in one TTI file, which is the standard way for TTI files, because the client can auto-rotate easily through each subpage using the cycle time (CT) value.

`http://petsciiproxy.nl/NOS-TT/705-3/T`

requests subpage 3 from page 705 from NOS-TT.

Note: if you omit the `/T' at the end of the URL, you will get the page in the format used by the Teletext 64 Ultimate client program.

## Services

Here is a list of services names:

| SERVICE | Name |
|---|---|
| ARD-TEXT | ARD Text |
| ZDF-TEXT | ZDF Text |
| ZDFINFO | ZDF Info |
| ZDFNEO | ZDF Neo |
| WDR-TEXT | WDR Text |
| NDR | NDR Text |
| HR-TEXT | HR-text |
| SWR-BW | SWR Baden-Wb |
| SWR-RP | SWR Rhein-Pf |
| 3SAT | 3SAT |
| ORF1 | ORF 1 |
| ORF2 | ORF 2 |
| ORF3 | ORF III |
| ORFSPORT | ORF Sport+ |
| SRF1 | SRF 1 |
| SRF2 | SRF zwei |
| SRFINFO | SRF Info |
| FORUM64 | Forum64 |
| CEEFAX | Ceefax |
| TEEFAX | Teefax |
| CHUNKYTEXT | Chunkytext |
| SPARK | SPARK |
| WEBFAX1 | Webfax 1 |
| WEBFAX2 | Webfax 2 |
| DR-TEKST-TV | DR Tekst-TV |
| SVT-TEXT | SVT Text |
| TEKSTI-TV | YLE Teksti-TV |
| HBN-TEKSTI-TV | HBN Teksti-TV |
| BMN1 | Bollentekst |
| MTVA-TXT | MTVA Teletext |
| NOS-TT | NOS Teletekst |
| NOSNEWS | NOS Nieuws |
| TELETEKSTARCHIEF | NOS Archief |
| RTS1 | RTS 1 |
| RTS2 | RTS 2 |
| RSILA1 | RSI LA 1 |
| RSILA2 | RSI LA 2 |
| TLGZ-TG1 | Telegazeta TVP 1 |
| TLGZ-TG2 | Telegazeta TVP 2 |
| TLGZ-OTV | Telegazeta TVP 3 |
| TLGZ-KUL | Telegazeta Kultura |
| TLGZ-HIS | Telegazeta Historia |
| TLGZ-SPO | Telegazeta Sport |
| TLGZ-SAT | Telegazeta Polonio |
| ANTENA3 | Antena 3 |
| LASEXTA | laSexta |
| RTP | RTP Teletexto |
| TELETEXT64 | Teletext 64 |

same list:

'ARD-TEXT', 'ZDF-TEXT', 'ZDFINFO', 'ZDFNEO', 'WDR-TEXT', 'NDR', 'HR-TEXT', 'SWR-BW', 'SWR-RP', '3SAT', 'ORF1', 'ORF2', 'ORF3', 'ORFSPORT', 'SRF1', 'SRF2', 'SRFINFO', 'FORUM64', 'CEEFAX', 'TEEFAX', 'CHUNKYTEXT', 'SPARK', 'WEBFAX1', 'WEBFAX2', 'DR-TEKST-TV', 'SVT-TEXT', 'TEKSTI-TV', 'HBN-TEKSTI-TV', 'BMN1', 'MTVA-TXT', 'NOS-TT', 'NOSNEWS', 'TELETEKSTARCHIEF', 'RTS1', 'RTS2', 'RSILA1', 'RSILA2', 'TLGZ-TG1', 'TLGZ-TG2', 'TLGZ-OTV', 'TLGZ-KUL', 'TLGZ-HIS', 'TLGZ-SPO', 'TLGZ-SAT', 'ANTENA3', 'LASEXTA', 'RTP', 'TELETEXT64'

## How does it look?
Because TTI is an ASCII file, you can also use this curl command in the terminal/command prompt:

`curl -v http://petsciiproxy.nl/NOS-TT/101/T` 

will give a result like this:

<pre>
DE,NOS-TT 101
PN,10100
SC,0000
PS,8200
CT,10,T
OL,0,       NOS-TT  101vri 20 nov  07:01:35
OL,1,|,||,||,,,,,,,,,,,,,,,,,,,,,,,,,,,,
OL,2,  /,|
          J  O  U  R  N  A  A  L  
OL,3,/ //,/,,/|,,,,,,,,,,,,,,,,,,,,,,,,,
OL,4,##############                         
OL,5,Politie Haiti bestrijdt bendegeweld129
OL,6,                                       
OL,7,Europa steunt Kyiv op 'tegentop'...127
OL,8,                                       
OL,9,Politie onderzoekt 'gluurappgroep'.105
OL,10,                                       
OL,11,Recordbeursgang voor Anthropic.....125
OL,12,                                       
OL,13,Veroordeling Britse 'Pelicot-zaak'.130
OL,14,                                       
OL,15,Jetten kijkt na inslag naar Rusland104
OL,16,                                       
OL,17,Schade ambassade NL Kyiv na inslag.126
OL,18,                                       
OL,19,Zes leden FVD weg na Zembla-docu...107
OL,20,                                       
OL,21,Raket treft hoogste gebouw in Riyad128
OL,22,                                       
OL,23,
      nieuwsoverzicht 102,103 - www.nos.nl 
OL,24,binnenland   sport  verkeer   weer
FL,102,601,730,702,0,100
</pre>
