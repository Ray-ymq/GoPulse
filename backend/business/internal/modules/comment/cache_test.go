package comment

import (
	"testing"
	"time"
)

func TestCachedCommentPagePreservesEmptyPageAndCursor(t *testing.T) {
	const postID = 41
	if !validCachedPage(postID, Page{Comments: []Comment{}}) || validCachedPage(postID, Page{}) {
		t.Fatal("空评论页应当命中，缺少评论数组的值应当回源")
	}
	comments := make([]Comment, DefaultLimit+1)
	for i := range comments {
		comments[i] = Comment{ID: uint64(len(comments) - i), PostID: postID, Content: "评论",
			CreatedAt: time.Now(), Author: Author{ID: 7, Username: "author"}}
	}
	page, err := pageFromComments(comments, DefaultLimit)
	if err != nil || page.NextCursor == nil || !validCachedPage(postID, page) {
		t.Fatalf("默认评论页为 %+v，错误为 %v", page, err)
	}
	if validCachedPage(postID+1, page) {
		t.Fatal("其他帖子的评论页应当回源")
	}
	cursor, err := EncodeCursor(Cursor{ID: comments[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	page.NextCursor = &cursor
	if validCachedPage(postID, page) {
		t.Fatal("与最后一条评论不匹配的游标应当回源")
	}
}
