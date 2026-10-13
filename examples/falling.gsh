#define MINO_SIZE 16
#define COLS 10
#define VISIBLE_ROWS 20
#define BUFFER_ROWS 2
#define ROWS 22  // 0, 1行目が22段目・21段目（画面外）、2〜21行目が画面内20行

#define RIGHT 1
#define LEFT -1
#define DAS 11
#define ARR 3
#define MAX_LOCK_RESETS 15

// 配列添字
// → P_TYPE, P_ROT, P_COL, P_ROW, P_BLOCKS
#define P_TYPE 0
#define P_ROT
#define P_COL
#define P_ROW
#define P_BLOCKS

// ゲーム状態
// → STATE_PLAY, STATE_PAUSE, STATE_GAMEOVER
#define STATE_PLAY 0
#define STATE_PAUSE
#define STATE_GAMEOVER

// ミノの種類
// → PIECE_I, PIECE_O, PIECE_T, PIECE_S, PIECE_Z, PIECE_J, PIECE_L
#define PIECE_I 0
#define PIECE_O
#define PIECE_T
#define PIECE_S
#define PIECE_Z
#define PIECE_J
#define PIECE_L

// ガイドライン準拠 落下速度テーブル (Lv 1 〜 Lv 15+) [フレーム数]
speed_table = [60, 50, 42, 34, 27, 21, 16, 12, 9, 6, 5, 4, 3, 2, 1]

// SRS準拠 各ミノの4回転状態 [type][rot][y][x]
piece_shapes = [
    // 0: I
    [
        [[0,0,0,0],[1,1,1,1],[0,0,0,0],[0,0,0,0]],
        [[0,0,1,0],[0,0,1,0],[0,0,1,0],[0,0,1,0]],
        [[0,0,0,0],[0,0,0,0],[1,1,1,1],[0,0,0,0]],
        [[0,1,0,0],[0,1,0,0],[0,1,0,0],[0,1,0,0]]
    ],
    // 1: O
    [
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]]
    ],
    // 2: T
    [
        [[0,1,0,0],[1,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,1,0],[0,1,0,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,1,0],[0,1,0,0],[0,0,0,0]],
        [[0,1,0,0],[1,1,0,0],[0,1,0,0],[0,0,0,0]]
    ],
    // 3: S
    [
        [[0,1,1,0],[1,1,0,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,1,0],[0,0,1,0],[0,0,0,0]],
        [[0,0,0,0],[0,1,1,0],[1,1,0,0],[0,0,0,0]],
        [[1,0,0,0],[1,1,0,0],[0,1,0,0],[0,0,0,0]]
    ],
    // 4: Z
    [
        [[1,1,0,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,0,1,0],[0,1,1,0],[0,1,0,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,0,0],[0,1,1,0],[0,0,0,0]],
        [[0,1,0,0],[1,1,0,0],[1,0,0,0],[0,0,0,0]]
    ],
    // 5: J
    [
        [[1,0,0,0],[1,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,0,0],[0,1,0,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,1,0],[0,0,1,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,0,0],[1,1,0,0],[0,0,0,0]]
    ],
    // 6: L
    [
        [[0,0,1,0],[1,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,0,0],[0,1,1,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,1,0],[1,0,0,0],[0,0,0,0]],
        [[1,1,0,0],[0,1,0,0],[0,1,0,0],[0,0,0,0]]
    ]
]

// SRS ウォールキックデータ
kick_jlstz = [
    [[0,0], [-1,0], [-1,-1], [0,2], [-1,2]],  // 0 -> 1
    [[0,0], [1,0], [1,1], [0,-2], [1,-2]],    // 1 -> 0
    [[0,0], [1,0], [1,1], [0,-2], [1,-2]],    // 1 -> 2
    [[0,0], [-1,0], [-1,-1], [0,2], [-1,2]],  // 2 -> 1
    [[0,0], [1,0], [1,-1], [0,2], [1,2]],     // 2 -> 3
    [[0,0], [-1,0], [-1,1], [0,-2], [-1,-2]], // 3 -> 2
    [[0,0], [-1,0], [-1,1], [0,-2], [-1,-2]], // 3 -> 0
    [[0,0], [1,0], [1,-1], [0,2], [1,2]]      // 0 -> 3
]

kick_i = [
    [[0,0], [-2,0], [1,0], [-2,1], [1,-2]],   // 0 -> 1
    [[0,0], [2,0], [-1,0], [2,-1], [-1,2]],   // 1 -> 0
    [[0,0], [-1,0], [2,0], [-1,-2], [2,1]],   // 2 -> 1
    [[0,0], [1,0], [-2,0], [1,2], [-2,-1]],   // 2 -> 3
    [[0,0], [2,0], [-1,0], [2,-1], [-1,2]],   // 3 -> 2
    [[0,0], [-2,0], [1,0], [-2,1], [1,-2]],   // 3 -> 0
    [[0,0], [1,0], [-2,0], [1,2], [-2,-1]],   // 0 -> 3
    [[0,0], [-1,0], [2,0], [-1,-2], [2,1]]
]

// フィールド
field = dim(ROWS, COLS)
repeat ROWS as r {
    repeat COLS as c {
        field[r][c] = 0
    }
}

// 消去ラインフラグ
clearing_lines = dim(ROWS)
repeat ROWS as r {
    clearing_lines[r] = 0
}
clear_timer = 0

// 7-Bag 乱数生成器
bag = dim(7)
bag_index = 7

// ゲーム変数
game_state = STATE_PLAY
score = 0
lines_cleared = 0
level = 1
is_running = true

// ピース情報
// 配列で表現: [P_TYPE, P_ROT, P_COL, P_ROW, P_BLOCKS]
// 型を混ぜられるので、整数と配列を同じ配列に入れられる
piece = dim(5)
piece[P_TYPE]   = PIECE_I
piece[P_ROT]    = 0
piece[P_COL]    = 3
piece[P_ROW]    = 0
piece[P_BLOCKS] = piece_shapes[PIECE_I][0]

// タイマー・カウンタ
right_das = 0
left_das = 0
arr_timer = 0
gravity_timer = 0
gravity_interval = 60
lock_delay = 30
lock_reset_count = 0
lowest_row = 0
mino_img = 0
sfx_move = 0

// キーフラグ
was_right_pressed = false
was_left_pressed = false
was_up_pressed = false
was_down_pressed = false
was_z_pressed = false
was_x_pressed = false
was_space_pressed = false
was_esc_pressed = false

def get_kick_id(from_rot, to_rot) {
    if from_rot == 0 && to_rot == 1 { return 0 }
    if from_rot == 1 && to_rot == 0 { return 1 }
    if from_rot == 1 && to_rot == 2 { return 2 }
    if from_rot == 2 && to_rot == 1 { return 3 }
    if from_rot == 2 && to_rot == 3 { return 4 }
    if from_rot == 3 && to_rot == 2 { return 5 }
    if from_rot == 3 && to_rot == 0 { return 6 }
    if from_rot == 0 && to_rot == 3 { return 7 }
    return
}

def get_next_piece_type() {
    if bag_index >= 7 {
        repeat 7 as i {
            bag[i] = i
        }
        repeat 7 as i {
            swap_idx = rnd(7 - i) + i
            tmp = bag[i]
            bag[i] = bag[swap_idx]
            bag[swap_idx] = tmp
        }
        bag_index = 0
    }
    next_type = bag[bag_index]
    bag_index += 1
    return next_type
}

def update_speed() {
    idx = level - 1
    if idx > 14 {
        idx = 14
    }
    gravity_interval = speed_table[idx]
}

def init_game() {
    repeat ROWS as r {
        clearing_lines[r] = 0
        repeat COLS as c {
            field[r][c] = 0
        }
    }
    clear_timer = 0
    score = 0
    lines_cleared = 0
    level = 1
    update_speed()
    gravity_timer = 0
    bag_index = 7
    game_state = STATE_PLAY
    spawn_piece()
}

// 21段目・22段目へのスポーン処理
def spawn_piece() {
    new_type = get_next_piece_type()
    piece[P_TYPE]   = new_type
    piece[P_ROT]    = 0
    piece[P_COL]    = 3
    piece[P_ROW]    = 0   // row 0: 22段目, row 1: 21段目
    piece[P_BLOCKS] = piece_shapes[new_type][0]

    gravity_timer = 0
    lock_delay = 30
    lock_reset_count = 0

    // Block Out: スポーン位置に置けない場合はゲームオーバー
    if can_put(piece[P_BLOCKS], piece[P_COL], piece[P_ROW]) == false {
        game_state = STATE_GAMEOVER
        return
    }

    // ガイドライン仕様: 生成直後、障害物がなければ即座に1マス落下して視界へ入る
    if can_move(0, 1) {
        piece[P_ROW] += 1
    }

    lowest_row = piece[P_ROW]
}

def can_put(blocks, c, r) {
    repeat 4 as y {
        repeat 4 as x {
            if blocks[y][x] == 1 {
                target_col = c + x
                target_row = r + y
                if (target_col < 0) || (COLS <= target_col) {
                    return false
                }
                if (target_row < 0) || (ROWS <= target_row) {
                    return false
                }
                if field[target_row][target_col] != 0 {
                    return false
                }
            }
        }
    }
    return true
}

def can_move(dx, dy) {
    return can_put(piece[P_BLOCKS], piece[P_COL] + dx, piece[P_ROW] + dy)
}

def notify_lock_reset() {
    if can_move(0, 1) == false {
        if lock_reset_count < MAX_LOCK_RESETS {
            lock_delay = 30
            lock_reset_count += 1
        }
    }
}

def drop_piece() {
    piece[P_ROW] += 1
    if piece[P_ROW] > lowest_row {
        lowest_row = piece[P_ROW]
        lock_reset_count = 0
    }
}

def soft_drop() {
    if can_move(0, 1) {
        drop_piece()
        score += 1
        lock_delay = 30
    }
}

def hard_drop() {
    drop_dist = 0
    while can_move(0, 1) {
        drop_piece()
        drop_dist += 1
    }
    score += drop_dist * 2
    lock_piece()
}

def rotate(dir) {
    if piece[P_TYPE] == PIECE_O {
        return
    }

    old_rot = piece[P_ROT]
    new_rot = (old_rot + dir + 4) % 4
    next_blocks = piece_shapes[piece[P_TYPE]][new_rot]
    kick_id = get_kick_id(old_rot, new_rot)

    repeat 5 as i {
        test_dx = 0
        test_dy = 0
        if piece[P_TYPE] == PIECE_I {
            test_dx = kick_i[kick_id][i][0]
            test_dy = kick_i[kick_id][i][1]
        } else {
            test_dx = kick_jlstz[kick_id][i][0]
            test_dy = kick_jlstz[kick_id][i][1]
        }

        if can_put(next_blocks, piece[P_COL] + test_dx, piece[P_ROW] + test_dy) {
            piece[P_COL] += test_dx
            piece[P_ROW] += test_dy
            piece[P_ROT] = new_rot
            piece[P_BLOCKS] = next_blocks
            notify_lock_reset()
            break
        }
    }
}

def lock_piece() {
    // Lock Out 判定: 全ブロックが画面外（21段目以上: r < BUFFER_ROWS）で固定されたらゲームオーバー
    all_above = true
    repeat 4 as y {
        repeat 4 as x {
            if piece[P_BLOCKS][y][x] == 1 {
                if piece[P_ROW] + y >= BUFFER_ROWS {
                    all_above = false
                }
            }
        }
    }
    if all_above {
        game_state = STATE_GAMEOVER
        return
    }

    fix_piece()

    // 揃っているラインの検出
    has_line = false
    repeat ROWS as r {
        clearing_lines[r] = 0
        is_full = true
        repeat COLS as c {
            if field[r][c] == 0 {
                is_full = false
                break
            }
        }
        if is_full {
            clearing_lines[r] = 1
            has_line = true
        }
    }

    if has_line {
        clear_timer = 10
    } else {
        spawn_piece()
    }
}

def fix_piece() {
    repeat 4 as y {
        repeat 4 as x {
            if piece[P_BLOCKS][y][x] == 1 {
                target_y = piece[P_ROW] + y
                target_x = piece[P_COL] + x
                if target_y >= 0 && target_y < ROWS && target_x >= 0 && target_x < COLS {
                    field[target_y][target_x] = 1
                }
            }
        }
    }
}

def finish_line_clear() {
    cleared_count = 0
    r = ROWS - 1
    while r >= 0 {
        if clearing_lines[r] == 1 {
            cleared_count += 1
            shift_r = r
            while shift_r > 0 {
                repeat COLS as c {
                    field[shift_r][c] = field[shift_r - 1][c]
                }
                clearing_lines[shift_r] = clearing_lines[shift_r - 1]
                shift_r -= 1
            }
            repeat COLS as c {
                field[0][c] = 0
            }
            clearing_lines[0] = 0
        } else {
            r -= 1
        }
    }

    // 本家スコアリング (基本点 × 現在のレベル)
    if cleared_count == 1 { score += 100 * level }
    if cleared_count == 2 { score += 300 * level }
    if cleared_count == 3 { score += 500 * level }
    if cleared_count == 4 { score += 800 * level }

    lines_cleared += cleared_count

    // 10ライン消去ごとにレベルアップ＆落下速度更新
    new_level = (lines_cleared / 10) + 1
    if new_level != level {
        level = new_level
        update_speed()
    }

    repeat ROWS as i {
        clearing_lines[i] = 0
    }

    spawn_piece()
}

def handle_input() {
    // ESCキー (ポーズ / 終了)
    if getkey(27) {
        if was_esc_pressed == false {
            if game_state == STATE_PLAY {
                game_state = STATE_PAUSE
            } else {
                if game_state == STATE_PAUSE {
                    game_state = STATE_PLAY
                } else {
                    if game_state == STATE_GAMEOVER {
                        is_running = false
                    }
                }
            }
        }
        was_esc_pressed = true
    } else {
        was_esc_pressed = false
    }

    // ゲームオーバー画面操作
    if game_state == STATE_GAMEOVER {
        if getkey(82) { // Rキー
            init_game()
        }
        if getkey(81) { // Qキー
            is_running = false
        }
        return
    }

    if game_state == STATE_PAUSE || clear_timer > 0 {
        return
    }

    // 右矢印キー
    if getkey(39) {
        if was_right_pressed == false {
            if can_move(1, 0) {
                piece[P_COL] += 1
                notify_lock_reset()
            }
            right_das = 0
        } else {
            right_das += 1
            if right_das >= DAS && (right_das - DAS) % ARR == 0 {
                if can_move(1, 0) {
                    piece[P_COL] += 1
                    notify_lock_reset()
                }
            }
        }
        was_right_pressed = true
    } else {
        was_right_pressed = false
        right_das = 0
    }

    // 左矢印キー
    if getkey(37) {
        if was_left_pressed == false {
            if can_move(-1, 0) {
                piece[P_COL] -= 1
                notify_lock_reset()
            }
            left_das = 0
        } else {
            left_das += 1
            if left_das >= DAS && (left_das - DAS) % ARR == 0 {
                if can_move(-1, 0) {
                    piece[P_COL] -= 1
                    notify_lock_reset()
                }
            }
        }
        was_left_pressed = true
    } else {
        was_left_pressed = false
        left_das = 0
    }

    // 上矢印キー (右回転)
    if getkey(38) {
        if was_up_pressed == false {
            rotate(RIGHT)
        }
        was_up_pressed = true
    } else {
        was_up_pressed = false
    }

    // 下矢印キー (ソフトドロップ)
    if getkey(40) {
        arr_timer += 1
        if arr_timer >= ARR {
            arr_timer = 0
            soft_drop()
        }
        was_down_pressed = true
    } else {
        was_down_pressed = false
    }

    // Zキー (左回転)
    if getkey(90) {
        if was_z_pressed == false {
            rotate(LEFT)
        }
        was_z_pressed = true
    } else {
        was_z_pressed = false
    }

    // Xキー (右回転)
    if getkey(88) {
        if was_x_pressed == false {
            rotate(RIGHT)
        }
        was_x_pressed = true
    } else {
        was_x_pressed = false
    }

    // スペースキー (ハードドロップ)
    if getkey(32) {
        if was_space_pressed == false {
            hard_drop()
        }
        was_space_pressed = true
    } else {
        was_space_pressed = false
    }
}

def update() {
    if game_state != STATE_PLAY {
        return
    }

    if clear_timer > 0 {
        clear_timer -= 1
        if clear_timer == 0 {
            finish_line_clear()
        }
        return
    }

    // 接地中のロック猶予カウント
    if can_move(0, 1) == false {
        lock_delay -= 1
        if lock_delay <= 0 {
            lock_piece()
            return
        }
    }

    // 通常落下
    gravity_timer += 1
    if gravity_timer >= gravity_interval {
        gravity_timer = 0
        if can_move(0, 1) {
            drop_piece()
        }
    }
}

// 画面内（r >= BUFFER_ROWS）のみ描画
def draw_block(c, r) {
    if r < BUFFER_ROWS {
        return
    }
    px = c * MINO_SIZE
    py = (r - BUFFER_ROWS) * MINO_SIZE
    pos(px, py)
    gcopy(mino_img, 0, 0, MINO_SIZE, MINO_SIZE)
}

def draw_field() {
    repeat ROWS as r {
        if r >= BUFFER_ROWS {
            if clear_timer > 0 && clearing_lines[r] == 1 {
                color(255, 255, 255)
                boxf(0, (r - BUFFER_ROWS) * MINO_SIZE, COLS * MINO_SIZE, (r - BUFFER_ROWS + 1) * MINO_SIZE)
            } else {
                repeat COLS as c {
                    if field[r][c] != 0 {
                        draw_block(c, r)
                    }
                }
            }
        }
    }
}

def draw_piece() {
    repeat 4 as y {
        repeat 4 as x {
            if piece[P_BLOCKS][y][x] == 1 {
                draw_block(piece[P_COL] + x, piece[P_ROW] + y)
            }
        }
    }
}

def draw() {
    cls()
    draw_field()

    if game_state == STATE_PLAY && clear_timer == 0 {
        draw_piece()
    }

    // 常時画面最上部にステータスを小さく表示
    color()
    pos(4, 2)
    mes("Lv:" + level + " Sc:" + score)

    // ポーズ表示
    if game_state == STATE_PAUSE {
        color()
        pos(36, 120)
        mes("== PAUSED ==")
        pos(20, 150)
        mes("Press ESC to Resume")
    }

    // ゲームオーバー表示
    if game_state == STATE_GAMEOVER {
        color(255, 60, 60)
        pos(38, 80)
        mes("GAME OVER")

        color()
        pos(32, 110)
        mes("Level: " + level)
        pos(32, 130)
        mes("Score: " + score)
        pos(32, 150)
        mes("Lines: " + lines_cleared)

        pos(32, 190)
        mes("[R] : Retry")
        pos(32, 210)
        mes("[Q] : Quit")
    }
}

def main() {
    // 画面サイズは拡張せず、元の 10×20 マス（160×320）を維持
    screen(MINO_SIZE * COLS, MINO_SIZE * VISIBLE_ROWS)
    title("Tetris")
    mino_img = picload("assets/mino.png")
    init_game()

    while is_running {
        handle_input()
        update()
        draw()
        await(1)
    }
    end()
}

main()
