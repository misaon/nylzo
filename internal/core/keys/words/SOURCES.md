# Passphrase word lists

Each file holds 2048 distinct words, one per line, for the passphrase generator of [protocol §2](../../../../docs/wiki/reference/Protocol.md#2-key-derivation-client). The web core carries the same words in `web/core/src/keys/words`, and `spec/vectors.json` holds the SHA-256 of every list.

| File | Source | License |
| --- | --- | --- |
| `en.txt`, `cs.txt`, `it.txt`, `pt-br.txt` | the [BIP-39](https://github.com/bitcoin/bips/blob/master/bip-0039.mediawiki) lists as published | MIT |
| `es.txt`, `fr.txt` | the BIP-39 lists with the accents removed; every word stays distinct | MIT |
| `ja.txt` | the BIP-39 Japanese list in NFC | MIT |
| `de.txt` | `de-2048-v1.txt` of [dys2p/wordlists-de](https://github.com/dys2p/wordlists-de) as published | CC0 1.0, also offered as Unlicense or BSD-3-Clause |
| `pl.txt`, `sk.txt`, `id.txt` | Nylzo: common words in their base form, 4 to 8 letters from `a` to `z`, without names or offensive words | MIT, as Nylzo |
| `zh-hans.txt` | Nylzo: common two-character words chosen from the dictionary of [jieba](https://github.com/fxsjy/jieba) | MIT, as Nylzo and jieba |

BIP-39 is by Marek Palatinus, Pavol Rusnak, Aaron Voisine and Sean Bowe and states: "This BIP falls under the MIT License." The jieba dictionary is Copyright (c) 2013 Sun Junyi under the MIT License. Both are used under these terms:

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
