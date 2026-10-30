// Hotspots are cursor-image pixels, independent of the scaled desktop video.
export function cursorStyle(cursor) {
  if (!cursor || cursor.width<=1 || cursor.height<=1) return 'none';
  return `url("${cursor.uri}") ${cursor.x} ${cursor.y}, default`;
}
export function observerPosition(position,screen,cursor) {
  return {left:`${100*position.x/screen.width}%`,top:`${100*position.y/screen.height}%`,transform:`translate(${-cursor.x}px, ${-cursor.y}px)`};
}
