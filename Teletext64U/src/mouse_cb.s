; mouse_cb.s
;
; - Mouse pointer callbacks for cc65 <mouse.h> driver
; - Adapted from cc65 libsrc/c64/mcbdefault.s (Ullrich von Bassewitz, 2004; zlib licence)
; - The 3 sprite shapes are defined here (hand made with https://www.spritemate.com/)
;
; cc65 mcbdefault.s assumes the KERNAL screen at $0400 in VIC bank 0. We use VIC bank 2 with SCREEN_RAM
; at $8000, so the six callbacks (hide/show/prep/draw/movex/movey) are here.
;
; Sprite pointers: last 8 bytes of SCREEN_RAM's 1K block ($83F8-$83FF); the program only uses offsets 0..999.
;
; Sprite data: $BF40-$BFFF, right after BITMAP ($A000 + 8000 bytes).
;
; Sprites 0-2 are stacked at the same position!
; black outline, grey highlight, red fill, just like the old Amiga Workbench 1.3 mouse pointer

    .export _mouse_tt_callbacks
    .export _mouse_tt_cursor_init

SPRITE_MEM      = $BF40                ; after BITMAP, outside $9000-$9FFF
SPRITE_MEM1     = SPRITE_MEM + 64
SPRITE_MEM2     = SPRITE_MEM + 128
SPRITE_PTR      = $83F8                ; SCREEN_RAM + $3F8 (sprite 0 pointer byte)
SPRITE_PTR1     = $83F9                ; sprite 1 pointer byte
SPRITE_PTR2     = $83FA                ; sprite 2 pointer byte

VIC_SPR0_X      = $D000
VIC_SPR0_Y      = $D001
VIC_SPR1_X      = $D002
VIC_SPR1_Y      = $D003
VIC_SPR2_X      = $D004
VIC_SPR2_Y      = $D005
VIC_SPR_HI_X    = $D010
VIC_SPR_ENA     = $D015
VIC_SPR0_COLOR  = $D027
VIC_SPR1_COLOR  = $D028
VIC_SPR2_COLOR  = $D029
VIC_SPR_MCOLOR  = $D01C                ; sprite multicolor-enable bits
VIC_SPR_EXPX    = $D01D                ; sprite X-expand bits
VIC_SPR_EXPY    = $D017                ; sprite Y-expand bits
VIC_SPR_PRIO    = $D01B                ; sprite-behind-background priority bits

; The three sprites always move together.
MOUSE_SPR_MASK  = $07                  ; sprites 0, 1, 2
MOUSE_SPR_NMASK = $F8

.rodata

; Sprite shapes made with SpriteMate. First 63 bytes of each are copied, byte 64 is padding

pointershape_outline:
.BYTE %11111100,%00000000,%00000000
.BYTE %10000010,%00000000,%00000000
.BYTE %10000010,%00000000,%00000000
.BYTE %10000100,%00000000,%00000000
.BYTE %10000010,%00000000,%00000000
.BYTE %10010001,%00000000,%00000000
.BYTE %01101000,%10000000,%00000000
.BYTE %00000100,%01000000,%00000000
.BYTE %00000010,%00100000,%00000000
.BYTE %00000001,%01000000,%00000000
.BYTE %00000000,%10000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000

pointershape_highlight:
.BYTE %00000000,%00000000,%00000000
.BYTE %01111100,%00000000,%00000000
.BYTE %00000100,%00000000,%00000000
.BYTE %00001000,%00000000,%00000000
.BYTE %00000100,%00000000,%00000000
.BYTE %00000010,%00000000,%00000000
.BYTE %00000001,%00000000,%00000000
.BYTE %00000000,%10000000,%00000000
.BYTE %00000000,%01000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00001010

pointershape_fill:
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %01111000,%00000000,%00000000
.BYTE %01110000,%00000000,%00000000
.BYTE %01111000,%00000000,%00000000
.BYTE %01101100,%00000000,%00000000
.BYTE %00000110,%00000000,%00000000
.BYTE %00000011,%00000000,%00000000
.BYTE %00000001,%10000000,%00000000
.BYTE %00000000,%10000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000000,%00000000,%00000000
.BYTE %00000010

pointercolor_outline:
    .byte $00 ; black
pointercolor_highlight:
    .byte $0f ; light grey
pointercolor_fill:
    .byte $02 ; red

; --------------------------------------------------------------------------
; void mouse_tt_cursor_init(void)
; Called once, after mouse_install() succeeded and VIC bank 2 is selected: copies the shapes and
; points the sprites at them. Does not enable them; show() does.

.code

.proc _mouse_tt_cursor_init
    ldx #62
copy0:
    lda pointershape_outline,x
    sta SPRITE_MEM,x
    dex
    bpl copy0
    ldx #62
copy1:
    lda pointershape_highlight,x
    sta SPRITE_MEM1,x
    dex
    bpl copy1
    ldx #62
copy2:
    lda pointershape_fill,x
    sta SPRITE_MEM2,x
    dex
    bpl copy2
    lda #<(SPRITE_MEM / 64)
    sta SPRITE_PTR
    lda #<(SPRITE_MEM1 / 64)
    sta SPRITE_PTR1
    lda #<(SPRITE_MEM2 / 64)
    sta SPRITE_PTR2
    lda pointercolor_outline
    sta VIC_SPR0_COLOR
    lda pointercolor_highlight
    sta VIC_SPR1_COLOR
    lda pointercolor_fill
    sta VIC_SPR2_COLOR
    ; hi-res (not multicolor) sprites
    lda #MOUSE_SPR_NMASK
    and VIC_SPR_MCOLOR
    sta VIC_SPR_MCOLOR
    ; known state: no sprite enabled, no expand/priority bits for ours
    lda #$00
    sta VIC_SPR_ENA
    lda VIC_SPR_EXPX
    and #MOUSE_SPR_NMASK
    sta VIC_SPR_EXPX
    lda VIC_SPR_EXPY
    and #MOUSE_SPR_NMASK
    sta VIC_SPR_EXPY
    lda VIC_SPR_PRIO
    and #MOUSE_SPR_NMASK
    sta VIC_SPR_PRIO
    rts
.endproc

; --------------------------------------------------------------------------
; These routines below are always called with interrupts disabled!
; --------------------------------------------------------------------------
; Hide mouse

hide:
    lda #MOUSE_SPR_NMASK
    and VIC_SPR_ENA
    sta VIC_SPR_ENA
    rts

; --------------------------------------------------------------------------
; Show mouse

show:
    lda #MOUSE_SPR_MASK
    ora VIC_SPR_ENA
    sta VIC_SPR_ENA
    ; Fall through

; --------------------------------------------------------------------------
; Prepare to move the mouse pointer

prep:
    rts

; --------------------------------------------------------------------------
; Draw mouse

draw:
    rts

; --------------------------------------------------------------------------
; Move mouse pointer X position to the value in a/x.

movex:
    clc
    adc #24                 ; X correction (screen border offset)
    sta VIC_SPR0_X
    sta VIC_SPR1_X
    sta VIC_SPR2_X
    txa
    adc #0
    bne hi_set
    lda VIC_SPR_HI_X
    and #MOUSE_SPR_NMASK
    sta VIC_SPR_HI_X
    rts
hi_set:
    lda VIC_SPR_HI_X
    ora #MOUSE_SPR_MASK
    sta VIC_SPR_HI_X
    rts

; --------------------------------------------------------------------------
; Move mouse pointer Y position to the value in a/x.

movey:
    clc
    adc #50                 ; Y correction (first visible line)
    sta VIC_SPR0_Y
    sta VIC_SPR1_Y
    sta VIC_SPR2_Y
    rts

; --------------------------------------------------------------------------
; Callback structure (struct mouse_callbacks - see <mouse.h>)

.rodata

_mouse_tt_callbacks:
    .addr   hide
    .addr   show
    .addr   prep
    .addr   draw
    .addr   movex
    .addr   movey
