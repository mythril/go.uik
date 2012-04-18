package uik

import (
	"image"
	"image/draw"
	"github.com/skelterjohn/go.wde"
)

type CompositeBlockRequest struct {
	CompositeRequest
	Block *Block
}

// The foundation type is for channeling events to children, and passing along
// draw calls.
type Foundation struct {
	Block
	
	Children    []*Block
	ChildrenBounds map[*Block]Bounds

	CompositeBlockRequests chan CompositeBlockRequest

	// this block currently has keyboard priority
	KeyboardBlock    *Block
}

func (f *Foundation) Initialize() {
	f.Block.Initialize()
	f.CompositeBlockRequests = make(chan CompositeBlockRequest)
	f.ChildrenBounds = map[*Block]Bounds{}
}

func (f *Foundation) RemoveBlock(b *Block) {
	if b.Parent != f {
		// TODO: log
		return
	}
	close(b.Compositor)
	b.Compositor = nil
	if bounds, ok := f.ChildrenBounds[b]; ok {
		RedrawEventChan(f.Redraw).Stack(RedrawEvent{
			bounds,
		})
	}
	delete(f.ChildrenBounds, b)
	b.Parent = nil
}

func (f *Foundation) PlaceBlock(b *Block, bounds Bounds) {
	if b.Parent == nil {
		f.Children = append(f.Children, b)
		b.Parent = f
	} else if b.Parent != f {
		b.Parent.RemoveBlock(b)
		b.Parent = f
	}
	f.ChildrenBounds[b] = bounds

	b.Compositor = make(chan CompositeRequest)
	go func(b *Block, blockCompositor chan CompositeRequest) {
		for cr := range blockCompositor {
			f.CompositeBlockRequests <- CompositeBlockRequest {
				CompositeRequest: cr,
				Block: b,
			}
		}
	}(b, b.Compositor)
	RedrawEventChan(f.Redraw).Stack(RedrawEvent{

	})
}

func (f *Foundation) BlockForCoord(p Coord) (b *Block) {
	// quad-tree one day?
	for _, bl := range f.Children {
		bbs, ok := f.ChildrenBounds[bl]
		if !ok {
			continue
		}
		if bbs.Contains(p) {
			b = bl
			return
		}
	}
	return
}

func (f *Foundation) DoCompositeBlockRequest(cbr CompositeBlockRequest) {
	b := cbr.Block
	bounds, ok := f.ChildrenBounds[b]
	if !ok {
		return
	}
	f.PrepareBuffer()
	draw.Draw(f.Buffer, bounds.Rectangle(), cbr.Buffer, image.Point{0, 0}, draw.Over)
	if f.Compositor != nil {
		f.Compositor <- CompositeRequest{
			Buffer: f.Buffer,
		}
	}
}

func (f *Foundation) DoRedraw(e RedrawEvent) {
	bgc := f.PrepareBuffer()
	f.DoPaint(bgc)
	for _, child := range f.Children {
		translatedDirty := e.Bounds
		bbs, ok := f.ChildrenBounds[child]
		if !ok { continue }

		translatedDirty.Min.X -= bbs.Min.X
		translatedDirty.Min.Y -= bbs.Min.Y

		RedrawEventChan(child.Redraw).Stack(RedrawEvent{translatedDirty})
	}
	if f.Compositor != nil {
		f.Compositor <- CompositeRequest{
			Buffer: f.Buffer,
		}
	}
}

// dispense events to children, as appropriate
func (f *Foundation) HandleEvents() {
	f.ListenedChannels[f.CloseEvents] = true
	f.ListenedChannels[f.MouseDownEvents] = true
	f.ListenedChannels[f.MouseUpEvents] = true

	var dragOriginBlocks = map[wde.Button]*Block{}
	// drag and up events for the same button get sent to the origin as well

	for {
		select {
		case e := <-f.CloseEvents:
			for _, b := range f.Children {
				b.allEventsIn <- e
			}
		case e := <-f.MouseDownEvents:
			b := f.BlockForCoord(e.Loc)
			bbs := f.ChildrenBounds[b]
			if b == nil {
				break
			}
			dragOriginBlocks[e.Which] = b
			e.Loc.X -= bbs.Min.X
			e.Loc.Y -= bbs.Min.Y
			b.allEventsIn <- e
		case e := <-f.MouseUpEvents:
			b := f.BlockForCoord(e.Loc)
			bbs := f.ChildrenBounds[b]
			if b != nil {
				be := e
				be.Loc.X -= bbs.Min.X
				be.Loc.Y -= bbs.Min.Y
				b.allEventsIn <- be
			}
			if origin, ok := dragOriginBlocks[e.Which]; ok && origin != b {
				oe := e
				obbs := f.ChildrenBounds[origin]
				oe.Loc.X -= obbs.Min.X
				oe.Loc.Y -= obbs.Min.Y
				origin.allEventsIn <- oe
			}
			delete(dragOriginBlocks, e.Which)

		case e := <-f.Redraw:
			f.DoRedraw(e)
		case cbr := <-f.CompositeBlockRequests:
			f.DoCompositeBlockRequest(cbr)
		}
	}
}