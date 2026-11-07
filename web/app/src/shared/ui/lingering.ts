// Keeps the last text while its element fades out, so the text does not vanish before the fade does.
export function lingering(): (text: string) => string {
  let last = '';
  return (text) => {
    if (text !== '') {
      last = text;
    }
    return last;
  };
}
